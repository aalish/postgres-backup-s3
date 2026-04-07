package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/runtimecfg"
	"github.com/neelgai/postgres-backup/internal/storage"
	"github.com/neelgai/postgres-backup/internal/store"
)

type App struct {
	cfg        config.DashboardConfig
	logger     *slog.Logger
	store      *store.Store
	auth       *AuthManager
	settings   *SettingsService
	backups    *BackupManager
	retention  *RetentionManager
	scheduler  *Scheduler
	templates  *Templates
	httpServer *http.Server
}

type pageData struct {
	Title         string
	Page          string
	Authenticated bool
	Username      string
	CSRFToken     string
	Version       string
	LoginError    string
}

type paginatedResponse[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
}

type backupRow struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	S3Path    string    `json:"s3Path"`
	Key       string    `json:"key"`
	Status    string    `json:"status"`
}

type overviewResponse struct {
	BackupCount     int                 `json:"backupCount"`
	CurrentJob      *store.BackupRun    `json:"currentJob,omitempty"`
	LatestRun       *store.BackupRun    `json:"latestRun,omitempty"`
	LatestRetention *store.RetentionRun `json:"latestRetention,omitempty"`
	RecentBackups   []backupRow         `json:"recentBackups"`
}

func New(ctx context.Context, cfg config.DashboardConfig, logger *slog.Logger, version string) (*App, error) {
	storeCtx, cancel := context.WithTimeout(ctx, cfg.Mongo.ConnectTimeout)
	defer cancel()

	dataStore, err := store.New(storeCtx, cfg.Mongo, logger)
	if err != nil {
		return nil, err
	}
	templates, err := LoadTemplates(version)
	if err != nil {
		return nil, err
	}

	app := &App{
		cfg:       cfg,
		logger:    logger,
		store:     dataStore,
		auth:      NewAuthManager(cfg.Auth, cfg.HTTP.CookieSecure, dataStore, logger),
		templates: templates,
	}
	defaultSettings := runtimecfg.Defaults(cfg)
	app.settings = NewSettingsService(defaultSettings, dataStore)
	notifier := NewWebhookNotifier(logger)
	app.backups = NewBackupManager(cfg.Base, app.settings, dataStore, logger, notifier, app.recordAudit)
	app.retention = NewRetentionManager(cfg.Base, app.settings, dataStore, logger, notifier, app.recordAudit)
	app.scheduler = NewScheduler(app.settings, app.backups, app.retention, logger, app.recordAudit)
	app.httpServer = &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      app.routes(),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	if err := a.scheduler.Start(ctx); err != nil {
		return err
	}

	a.logger.Info("dashboard server starting", "addr", a.cfg.HTTP.Addr)
	err := a.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	_ = ctx
	a.scheduler.Stop()

	serverCtx, cancel := context.WithTimeout(context.Background(), a.cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := a.httpServer.Shutdown(serverCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if err := a.backups.Wait(context.Background()); err != nil {
		return err
	}
	if err := a.retention.Wait(context.Background()); err != nil {
		return err
	}
	if err := a.store.Close(context.Background()); err != nil {
		return err
	}

	return nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(StaticFS())))
	mux.HandleFunc("GET /", a.handleRoot)
	mux.HandleFunc("GET /login", a.handleLoginPage)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.requireHTMLState(a.handleLogout))
	mux.HandleFunc("GET /dashboard", a.requireHTMLAuth(a.handleDashboardPage))
	mux.HandleFunc("GET /backups", a.requireHTMLAuth(a.handleBackupsPage))
	mux.HandleFunc("GET /settings", a.requireHTMLAuth(a.handleSettingsPage))
	mux.HandleFunc("GET /logs", a.requireHTMLAuth(a.handleLogsPage))

	mux.HandleFunc("GET /api/overview", a.requireAPIAuth(a.handleOverviewAPI))
	mux.HandleFunc("GET /api/backups", a.requireAPIAuth(a.handleBackupsAPI))
	mux.HandleFunc("GET /api/backup-runs", a.requireAPIAuth(a.handleBackupRunsAPI))
	mux.HandleFunc("GET /api/settings", a.requireAPIAuth(a.handleSettingsAPI))
	mux.HandleFunc("POST /api/settings", a.requireAPIState(a.handleSaveSettingsAPI))
	mux.HandleFunc("POST /api/backups/trigger", a.requireAPIState(a.handleTriggerBackupAPI))
	mux.HandleFunc("GET /api/retention-runs", a.requireAPIAuth(a.handleRetentionRunsAPI))
	mux.HandleFunc("GET /api/audit-events", a.requireAPIAuth(a.handleAuditEventsAPI))

	return a.withRecovery(a.withRequestLogging(mux))
}

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, err := a.auth.AuthenticateRequest(w, r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, r, http.StatusOK, "")
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.ValidateLoginCSRF(r); err != nil {
		a.renderLogin(w, r, http.StatusForbidden, "The login form expired. Please try again.")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderLogin(w, r, http.StatusBadRequest, "Unable to read the login form.")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	session, err := a.auth.Login(r.Context(), w, r, username, password)
	switch {
	case errors.Is(err, errRateLimited):
		a.recordAudit(r.Context(), "auth.rate_limited", username, "Login blocked by rate limiting", map[string]any{
			"remoteAddr": r.RemoteAddr,
		})
		a.renderLogin(w, r, http.StatusTooManyRequests, "Too many login attempts. Please wait before trying again.")
		return
	case errors.Is(err, errUnauthorized):
		a.recordAudit(r.Context(), "auth.login_failed", username, "Login failed", map[string]any{
			"remoteAddr": r.RemoteAddr,
		})
		a.renderLogin(w, r, http.StatusUnauthorized, "Invalid username or password.")
		return
	case err != nil:
		a.logger.Error("login failed unexpectedly", "error", err)
		a.renderLogin(w, r, http.StatusInternalServerError, "Login is temporarily unavailable.")
		return
	}

	a.recordAudit(r.Context(), "auth.login_succeeded", session.Username, "Login succeeded", map[string]any{
		"remoteAddr": r.RemoteAddr,
	})
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request, user UserSession) {
	if err := a.auth.Logout(r.Context(), w, r); err != nil {
		a.logger.Error("logout failed", "error", err, "user", user.Username)
	}
	a.recordAudit(r.Context(), "auth.logout", user.Username, "User logged out", nil)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) handleDashboardPage(w http.ResponseWriter, r *http.Request, user UserSession) {
	a.renderPage(w, "dashboard", pageData{
		Title:         "Dashboard",
		Page:          "dashboard",
		Authenticated: true,
		Username:      user.Username,
		CSRFToken:     user.CSRFToken,
		Version:       a.templates.Version,
	})
}

func (a *App) handleBackupsPage(w http.ResponseWriter, r *http.Request, user UserSession) {
	a.renderPage(w, "backups", pageData{
		Title:         "Backups",
		Page:          "backups",
		Authenticated: true,
		Username:      user.Username,
		CSRFToken:     user.CSRFToken,
		Version:       a.templates.Version,
	})
}

func (a *App) handleSettingsPage(w http.ResponseWriter, r *http.Request, user UserSession) {
	a.renderPage(w, "settings", pageData{
		Title:         "Settings",
		Page:          "settings",
		Authenticated: true,
		Username:      user.Username,
		CSRFToken:     user.CSRFToken,
		Version:       a.templates.Version,
	})
}

func (a *App) handleLogsPage(w http.ResponseWriter, r *http.Request, user UserSession) {
	a.renderPage(w, "logs", pageData{
		Title:         "Logs",
		Page:          "logs",
		Authenticated: true,
		Username:      user.Username,
		CSRFToken:     user.CSRFToken,
		Version:       a.templates.Version,
	})
}

func (a *App) handleOverviewAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	_ = user
	backups, err := a.loadBackups(r.Context(), r.URL.Query().Get("namePrefix"), "", "", "", "createdAt", "desc")
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	latestRun, err := a.store.LatestBackupRun(r.Context())
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	retentionRuns, _, err := a.store.ListRetentionRuns(r.Context(), 1, 1)
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}

	resp := overviewResponse{
		BackupCount:   len(backups),
		CurrentJob:    a.backups.Current(),
		LatestRun:     latestRun,
		RecentBackups: sliceRows(backups, 5),
	}
	if len(retentionRuns) > 0 {
		resp.LatestRetention = &retentionRuns[0]
	}
	a.writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleBackupsAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	_ = user
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 20)
	if pageSize > 100 {
		pageSize = 100
	}

	rows, err := a.loadBackups(
		r.Context(),
		r.URL.Query().Get("namePrefix"),
		r.URL.Query().Get("status"),
		r.URL.Query().Get("from"),
		r.URL.Query().Get("to"),
		r.URL.Query().Get("sort"),
		r.URL.Query().Get("order"),
	)
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}

	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start > len(rows) {
		start = len(rows)
	}
	end := start + pageSize
	if end > len(rows) {
		end = len(rows)
	}

	resp := paginatedResponse[backupRow]{
		Items:      rows[start:end],
		Page:       page,
		PageSize:   pageSize,
		Total:      int64(len(rows)),
		TotalPages: totalPages(int64(len(rows)), pageSize),
	}
	a.writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleBackupRunsAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	_ = user
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 20)
	items, total, err := a.store.ListBackupRuns(r.Context(), page, pageSize)
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	a.writeJSON(w, http.StatusOK, paginatedResponse[store.BackupRun]{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	})
}

func (a *App) handleSettingsAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	_ = user
	settings, err := a.settings.GetActive(r.Context())
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	a.writeJSON(w, http.StatusOK, settings)
}

func (a *App) handleSaveSettingsAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	var input runtimecfg.Settings
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		a.writeAPIErrorMessage(w, http.StatusBadRequest, "Invalid settings payload.")
		return
	}
	input = sanitizeSettings(input)
	settings, err := a.settings.Save(r.Context(), input, user.Username)
	if err != nil {
		a.writeAPIErrorMessage(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.scheduler.ApplySettings(settings, user.Username); err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}

	a.recordAudit(r.Context(), "config.updated", user.Username, "Runtime settings updated", map[string]any{
		"backupSchedule": settings.BackupSchedule,
		"retentionDays":  settings.RetentionDays,
		"s3Bucket":       settings.S3Bucket,
		"s3Prefix":       settings.S3Prefix,
	})
	a.writeJSON(w, http.StatusOK, map[string]any{
		"message":  "Settings saved successfully.",
		"settings": settings,
	})
}

func (a *App) handleTriggerBackupAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	run, err := a.backups.Trigger(r.Context(), user.Username, "manual")
	if errors.Is(err, errBackupRunning) {
		a.writeJSON(w, http.StatusConflict, map[string]any{
			"message": "A backup is already in progress.",
			"run":     run,
		})
		return
	}
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}

	a.writeJSON(w, http.StatusAccepted, map[string]any{
		"message": "Backup started.",
		"run":     run,
	})
}

func (a *App) handleRetentionRunsAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	_ = user
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 20)
	items, total, err := a.store.ListRetentionRuns(r.Context(), page, pageSize)
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	a.writeJSON(w, http.StatusOK, paginatedResponse[store.RetentionRun]{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	})
}

func (a *App) handleAuditEventsAPI(w http.ResponseWriter, r *http.Request, user UserSession) {
	_ = user
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "pageSize", 20)
	items, total, err := a.store.ListAuditEvents(r.Context(), page, pageSize)
	if err != nil {
		a.writeAPIError(w, http.StatusInternalServerError, err)
		return
	}
	a.writeJSON(w, http.StatusOK, paginatedResponse[store.AuditEvent]{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages(total, pageSize),
	})
}

func (a *App) requireHTMLAuth(next func(http.ResponseWriter, *http.Request, UserSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.AuthenticateRequest(w, r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r, user)
	}
}

func (a *App) requireHTMLState(next func(http.ResponseWriter, *http.Request, UserSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.AuthenticateRequest(w, r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if err := a.auth.VerifyCSRF(r, user); err != nil {
			http.Error(w, "Invalid CSRF token.", http.StatusForbidden)
			return
		}
		next(w, r, user)
	}
}

func (a *App) requireAPIAuth(next func(http.ResponseWriter, *http.Request, UserSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.AuthenticateRequest(w, r)
		if err != nil {
			a.writeAPIErrorMessage(w, http.StatusUnauthorized, "Authentication required.")
			return
		}
		next(w, r, user)
	}
}

func (a *App) requireAPIState(next func(http.ResponseWriter, *http.Request, UserSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.AuthenticateRequest(w, r)
		if err != nil {
			a.writeAPIErrorMessage(w, http.StatusUnauthorized, "Authentication required.")
			return
		}
		if err := a.auth.VerifyCSRF(r, user); err != nil {
			a.writeAPIErrorMessage(w, http.StatusForbidden, "Invalid CSRF token.")
			return
		}
		next(w, r, user)
	}
}

func (a *App) renderLogin(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := pageData{
		Title:         "Sign In",
		Page:          "login",
		Authenticated: false,
		CSRFToken:     a.auth.EnsureLoginCSRF(w, r),
		LoginError:    message,
		Version:       a.templates.Version,
	}
	w.WriteHeader(status)
	if err := a.templates.Render(w, "login", data); err != nil {
		a.logger.Error("render login page", "error", err)
	}
}

func (a *App) renderPage(w http.ResponseWriter, name string, data pageData) {
	if err := a.templates.Render(w, name, data); err != nil {
		a.logger.Error("render page", "page", name, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (a *App) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		a.logger.Error("write JSON response", "error", err)
	}
}

func (a *App) writeAPIError(w http.ResponseWriter, status int, err error) {
	a.writeAPIErrorMessage(w, status, err.Error())
}

func (a *App) writeAPIErrorMessage(w http.ResponseWriter, status int, message string) {
	a.writeJSON(w, status, map[string]string{"error": message})
}

func (a *App) loadBackups(ctx context.Context, namePrefix, statusFilter, fromRaw, toRaw, sortField, sortOrder string) ([]backupRow, error) {
	settings, err := a.settings.GetActive(ctx)
	if err != nil {
		return nil, err
	}
	jobCfg := settings.Apply(a.cfg.Base)
	uploader, err := storage.NewS3Uploader(ctx, jobCfg.S3, jobCfg.Retry, a.logger)
	if err != nil {
		return nil, err
	}
	objects, err := uploader.ListObjects(ctx)
	if err != nil {
		return nil, err
	}

	var fromTime, toTime time.Time
	if strings.TrimSpace(fromRaw) != "" {
		fromTime, err = time.Parse("2006-01-02", fromRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid from date")
		}
	}
	if strings.TrimSpace(toRaw) != "" {
		toTime, err = time.Parse("2006-01-02", toRaw)
		if err != nil {
			return nil, fmt.Errorf("invalid to date")
		}
		toTime = toTime.Add(24*time.Hour - time.Nanosecond)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(settings.RetentionDays) * 24 * time.Hour)
	warnCutoff := cutoff.Add(24 * time.Hour)
	rows := make([]backupRow, 0, len(objects))
	for _, object := range objects {
		state := "available"
		switch {
		case object.LastModified.Before(cutoff):
			state = "expired"
		case object.LastModified.Before(warnCutoff):
			state = "expiring_soon"
		}

		if prefix := strings.TrimSpace(namePrefix); prefix != "" && !strings.HasPrefix(strings.ToLower(object.Filename), strings.ToLower(prefix)) {
			continue
		}
		if filter := strings.TrimSpace(statusFilter); filter != "" && filter != "all" && filter != state {
			continue
		}
		if !fromTime.IsZero() && object.LastModified.Before(fromTime) {
			continue
		}
		if !toTime.IsZero() && object.LastModified.After(toTime) {
			continue
		}

		rows = append(rows, backupRow{
			Name:      object.Filename,
			Size:      object.Size,
			CreatedAt: object.LastModified,
			S3Path:    object.S3URI,
			Key:       object.Key,
			Status:    state,
		})
	}

	desc := !strings.EqualFold(sortOrder, "asc")
	switch strings.TrimSpace(sortField) {
	case "", "createdAt":
		sort.Slice(rows, func(i, j int) bool {
			if desc {
				return rows[i].CreatedAt.After(rows[j].CreatedAt)
			}
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		})
	case "name":
		sort.Slice(rows, func(i, j int) bool {
			if desc {
				return rows[i].Name > rows[j].Name
			}
			return rows[i].Name < rows[j].Name
		})
	case "size":
		sort.Slice(rows, func(i, j int) bool {
			if desc {
				return rows[i].Size > rows[j].Size
			}
			return rows[i].Size < rows[j].Size
		})
	case "status":
		sort.Slice(rows, func(i, j int) bool {
			if desc {
				return rows[i].Status > rows[j].Status
			}
			return rows[i].Status < rows[j].Status
		})
	}

	return rows, nil
}

func (a *App) recordAudit(ctx context.Context, eventType, actor, message string, metadata map[string]any) {
	a.logger.Info("audit event", "type", eventType, "actor", actor, "message", message)
	event := store.AuditEvent{
		ID:        store.NewID(),
		Type:      eventType,
		Actor:     actor,
		Message:   message,
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
	}
	if err := a.store.InsertAuditEvent(ctx, event); err != nil {
		a.logger.Error("persist audit event", "type", eventType, "error", err)
	}
}

func (a *App) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		a.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration", time.Since(startedAt).String(),
		)
	})
}

func (a *App) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("panic recovered", "error", recovered, "path", r.URL.Path)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func sanitizeSettings(input runtimecfg.Settings) runtimecfg.Settings {
	input.ID = runtimecfg.DocumentID
	input.BackupSchedule = strings.TrimSpace(input.BackupSchedule)
	input.S3Bucket = strings.TrimSpace(input.S3Bucket)
	input.S3Prefix = strings.Trim(strings.TrimSpace(input.S3Prefix), "/")
	input.BackupOutputDir = strings.TrimSpace(input.BackupOutputDir)
	input.BackupFilenamePrefix = strings.TrimSpace(input.BackupFilenamePrefix)
	input.Notification.WebhookURL = strings.TrimSpace(input.Notification.WebhookURL)
	input.Notification.WebhookTimeout = strings.TrimSpace(input.Notification.WebhookTimeout)
	return input
}

func totalPages(total int64, pageSize int) int {
	if pageSize <= 0 {
		return 1
	}
	pages := int(math.Ceil(float64(total) / float64(pageSize)))
	if pages < 1 {
		return 1
	}
	return pages
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func sliceRows(rows []backupRow, n int) []backupRow {
	if len(rows) <= n {
		return rows
	}
	return rows[:n]
}
