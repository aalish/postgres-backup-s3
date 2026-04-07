package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/runtimecfg"
	"github.com/neelgai/postgres-backup/internal/service"
	"github.com/neelgai/postgres-backup/internal/storage"
	"github.com/neelgai/postgres-backup/internal/store"
)

var errBackupRunning = errors.New("backup already running")

type auditRecorder func(context.Context, string, string, string, map[string]any)

type SettingsService struct {
	defaults runtimecfg.Settings
	store    *store.Store
}

type WebhookNotifier struct {
	logger *slog.Logger
}

type BackupManager struct {
	base        config.Config
	settings    *SettingsService
	store       *store.Store
	logger      *slog.Logger
	notifier    *WebhookNotifier
	recordAudit auditRecorder

	mu      sync.RWMutex
	current *store.BackupRun
	wg      sync.WaitGroup
}

type RetentionManager struct {
	base        config.Config
	settings    *SettingsService
	store       *store.Store
	logger      *slog.Logger
	notifier    *WebhookNotifier
	recordAudit auditRecorder

	mu      sync.Mutex
	running bool
	wg      sync.WaitGroup
}

type Scheduler struct {
	settings    *SettingsService
	backups     *BackupManager
	retention   *RetentionManager
	logger      *slog.Logger
	recordAudit auditRecorder

	mu         sync.Mutex
	cron       *cron.Cron
	backupID   cron.EntryID
	stopCh     chan struct{}
	started    bool
	background sync.WaitGroup
}

func NewSettingsService(defaults runtimecfg.Settings, store *store.Store) *SettingsService {
	return &SettingsService{
		defaults: defaults,
		store:    store,
	}
}

func (s *SettingsService) GetActive(ctx context.Context) (runtimecfg.Settings, error) {
	settings, found, err := s.store.GetSettings(ctx)
	if err != nil {
		return runtimecfg.Settings{}, err
	}
	if found {
		return settings, nil
	}

	defaults := s.defaults
	defaults.ID = runtimecfg.DocumentID
	return defaults, nil
}

func (s *SettingsService) Save(ctx context.Context, settings runtimecfg.Settings, actor string) (runtimecfg.Settings, error) {
	settings.ID = runtimecfg.DocumentID
	settings.UpdatedAt = time.Now().UTC()
	settings.UpdatedBy = actor
	if err := settings.Validate(); err != nil {
		return runtimecfg.Settings{}, err
	}
	if err := s.store.SaveSettings(ctx, settings); err != nil {
		return runtimecfg.Settings{}, err
	}
	return settings, nil
}

func NewWebhookNotifier(logger *slog.Logger) *WebhookNotifier {
	return &WebhookNotifier{logger: logger}
}

func (n *WebhookNotifier) NotifyFailure(ctx context.Context, settings runtimecfg.Settings, event, actor, message string, metadata map[string]any) {
	if !settings.Notification.Enabled || strings.TrimSpace(settings.Notification.WebhookURL) == "" {
		return
	}
	timeout, err := settings.NotificationTimeout()
	if err != nil {
		n.logger.Error("notification configuration invalid", "error", err)
		return
	}

	payload := map[string]any{
		"event":      event,
		"actor":      actor,
		"message":    message,
		"metadata":   metadata,
		"occurredAt": time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		n.logger.Error("marshal webhook payload", "error", err)
		return
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, settings.Notification.WebhookURL, bytes.NewReader(body))
	if err != nil {
		n.logger.Error("build webhook request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		n.logger.Error("send webhook notification", "error", err, "event", event)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		n.logger.Error("webhook returned non-success status", "status_code", resp.StatusCode, "event", event)
		return
	}
	n.logger.Info("webhook notification delivered", "event", event, "status_code", resp.StatusCode)
}

func NewBackupManager(base config.Config, settings *SettingsService, store *store.Store, logger *slog.Logger, notifier *WebhookNotifier, recorder auditRecorder) *BackupManager {
	return &BackupManager{
		base:        base,
		settings:    settings,
		store:       store,
		logger:      logger,
		notifier:    notifier,
		recordAudit: recorder,
	}
}

func (m *BackupManager) Trigger(ctx context.Context, actor, source string) (store.BackupRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current != nil && m.current.Status == "running" {
		return *m.current, errBackupRunning
	}

	settings, err := m.settings.GetActive(ctx)
	if err != nil {
		return store.BackupRun{}, err
	}

	now := time.Now().UTC()
	run := store.BackupRun{
		ID:            store.NewID(),
		TriggeredBy:   actor,
		TriggerSource: source,
		Status:        "running",
		StartedAt:     now,
	}
	if err := m.store.InsertBackupRun(ctx, run); err != nil {
		return store.BackupRun{}, err
	}

	m.current = &run
	m.wg.Add(1)
	go m.execute(run, settings)

	m.logger.Info("backup job queued", "run_id", run.ID, "triggered_by", actor, "trigger_source", source)
	if m.recordAudit != nil {
		m.recordAudit(context.Background(), "backup.started", actor, "Backup job started", map[string]any{
			"runId":         run.ID,
			"triggerSource": source,
		})
	}

	return run, nil
}

func (m *BackupManager) execute(run store.BackupRun, settings runtimecfg.Settings) {
	defer m.wg.Done()

	finishedAt := time.Now().UTC()
	defer func() {
		m.mu.Lock()
		if m.current != nil && m.current.ID == run.ID {
			m.current = nil
		}
		m.mu.Unlock()
	}()

	jobCfg := settings.Apply(m.base)
	svc, err := service.New(context.Background(), jobCfg, m.logger)
	if err == nil {
		var result service.Result
		result, err = svc.RunWithResult(context.Background())
		if err == nil {
			run.Status = "succeeded"
			run.ArchiveFilename = result.ArchiveFilename
			run.ArchivePath = result.ArchivePath
			run.S3URI = result.S3URI
			run.S3Key = storage.BuildObjectKey(settings.S3Prefix, result.ArchiveFilename)
		}
	}

	finishedAt = time.Now().UTC()
	run.FinishedAt = &finishedAt
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		m.logger.Error("backup job failed", "run_id", run.ID, "error", err)
		if m.recordAudit != nil {
			m.recordAudit(context.Background(), "backup.failed", run.TriggeredBy, "Backup job failed", map[string]any{
				"runId": run.ID,
				"error": err.Error(),
			})
		}
		m.notifier.NotifyFailure(context.Background(), settings, "backup.failed", run.TriggeredBy, "Backup job failed", map[string]any{
			"runId": run.ID,
			"error": err.Error(),
		})
	} else {
		m.logger.Info("backup job completed", "run_id", run.ID, "s3_uri", run.S3URI)
		if m.recordAudit != nil {
			m.recordAudit(context.Background(), "backup.succeeded", run.TriggeredBy, "Backup job completed successfully", map[string]any{
				"runId": run.ID,
				"s3URI": run.S3URI,
			})
		}
	}

	if updateErr := m.store.UpdateBackupRun(context.Background(), run); updateErr != nil {
		m.logger.Error("persist backup run result", "run_id", run.ID, "error", updateErr)
	}
}

func (m *BackupManager) Current() *store.BackupRun {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.current == nil {
		return nil
	}
	copy := *m.current
	return &copy
}

func (m *BackupManager) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func NewRetentionManager(base config.Config, settings *SettingsService, store *store.Store, logger *slog.Logger, notifier *WebhookNotifier, recorder auditRecorder) *RetentionManager {
	return &RetentionManager{
		base:        base,
		settings:    settings,
		store:       store,
		logger:      logger,
		notifier:    notifier,
		recordAudit: recorder,
	}
}

func (m *RetentionManager) Run(ctx context.Context, trigger, actor string) (*store.RetentionRun, error) {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil, fmt.Errorf("retention job already running")
	}
	m.running = true
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		m.wg.Done()
	}()

	settings, err := m.settings.GetActive(ctx)
	if err != nil {
		return nil, err
	}

	startedAt := time.Now().UTC()
	run := store.RetentionRun{
		ID:        store.NewID(),
		Trigger:   trigger,
		Status:    "running",
		StartedAt: startedAt,
	}
	if err := m.store.InsertRetentionRun(ctx, run); err != nil {
		return nil, err
	}

	jobCfg := settings.Apply(m.base)
	uploader, err := storage.NewS3Uploader(ctx, jobCfg.S3, jobCfg.Retry, m.logger)
	if err == nil {
		var objects []storage.ObjectInfo
		objects, err = uploader.ListObjects(ctx)
		if err == nil {
			run.Evaluated = len(objects)
			cutoff := time.Now().UTC().Add(-time.Duration(settings.RetentionDays) * 24 * time.Hour)
			for _, object := range objects {
				if object.LastModified.After(cutoff) {
					continue
				}
				if deleteErr := uploader.DeleteObject(ctx, object.Key); deleteErr != nil {
					err = deleteErr
					break
				}
				run.Deleted++
				run.DeletedKeys = append(run.DeletedKeys, object.Key)
				m.logger.Info("retention deleted expired backup", "key", object.Key, "last_modified", object.LastModified.Format(time.RFC3339))
				if m.recordAudit != nil {
					m.recordAudit(context.Background(), "retention.deleted_object", actor, "Deleted expired backup from S3", map[string]any{
						"key":          object.Key,
						"lastModified": object.LastModified.Format(time.RFC3339),
					})
				}
			}
		}
	}

	finishedAt := time.Now().UTC()
	run.FinishedAt = &finishedAt
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
		m.logger.Error("retention job failed", "run_id", run.ID, "trigger", trigger, "error", err)
		if m.recordAudit != nil {
			m.recordAudit(context.Background(), "retention.failed", actor, "Retention job failed", map[string]any{
				"runId": run.ID,
				"error": err.Error(),
			})
		}
		m.notifier.NotifyFailure(context.Background(), settings, "retention.failed", actor, "Retention job failed", map[string]any{
			"runId": run.ID,
			"error": err.Error(),
		})
	} else {
		run.Status = "succeeded"
		if m.recordAudit != nil {
			m.recordAudit(context.Background(), "retention.succeeded", actor, "Retention job completed", map[string]any{
				"runId":     run.ID,
				"evaluated": run.Evaluated,
				"deleted":   run.Deleted,
			})
		}
	}

	if updateErr := m.store.UpdateRetentionRun(context.Background(), run); updateErr != nil {
		m.logger.Error("persist retention run result", "run_id", run.ID, "error", updateErr)
	}

	return &run, err
}

func (m *RetentionManager) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func NewScheduler(settings *SettingsService, backups *BackupManager, retention *RetentionManager, logger *slog.Logger, recorder auditRecorder) *Scheduler {
	return &Scheduler{
		settings:    settings,
		backups:     backups,
		retention:   retention,
		logger:      logger,
		recordAudit: recorder,
		cron:        cron.New(),
		stopCh:      make(chan struct{}),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	settings, err := s.settings.GetActive(ctx)
	if err != nil {
		return err
	}
	if err := s.scheduleBackupLocked(settings); err != nil {
		return err
	}

	s.cron.Start()
	s.started = true
	s.background.Add(1)
	go s.retentionLoop()
	go s.runRetention("startup", "system")
	return nil
}

func (s *Scheduler) ApplySettings(settings runtimecfg.Settings, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.scheduleBackupLocked(settings); err != nil {
		return err
	}
	if s.started {
		go s.runRetention("config_change", actor)
	}

	return nil
}

func (s *Scheduler) scheduleBackupLocked(settings runtimecfg.Settings) error {
	if s.backupID != 0 {
		s.cron.Remove(s.backupID)
		s.backupID = 0
	}

	entryID, err := s.cron.AddFunc(settings.BackupSchedule, func() {
		if _, triggerErr := s.backups.Trigger(context.Background(), "scheduler", "scheduled"); triggerErr != nil {
			if errors.Is(triggerErr, errBackupRunning) {
				s.logger.Warn("scheduled backup skipped because another backup is running")
				if s.recordAudit != nil {
					s.recordAudit(context.Background(), "backup.skipped", "scheduler", "Scheduled backup skipped because another backup is already running", nil)
				}
				return
			}
			s.logger.Error("scheduled backup trigger failed", "error", triggerErr)
			if s.recordAudit != nil {
				s.recordAudit(context.Background(), "backup.schedule_error", "scheduler", "Failed to start scheduled backup", map[string]any{
					"error": triggerErr.Error(),
				})
			}
		}
	})
	if err != nil {
		return fmt.Errorf("schedule backup cron: %w", err)
	}

	s.backupID = entryID
	return nil
}

func (s *Scheduler) retentionLoop() {
	defer s.background.Done()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runRetention("periodic", "system")
		}
	}
}

func (s *Scheduler) runRetention(trigger, actor string) {
	if _, err := s.retention.Run(context.Background(), trigger, actor); err != nil {
		s.logger.Error("retention run failed", "trigger", trigger, "error", err)
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	close(s.stopCh)
	s.cron.Stop()
	s.started = false
	s.mu.Unlock()

	s.background.Wait()
}
