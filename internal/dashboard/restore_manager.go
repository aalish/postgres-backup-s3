package dashboard

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/restore"
	"github.com/neelgai/postgres-backup/internal/store"
	"go.mongodb.org/mongo-driver/bson"
)

type RestoreManager struct {
	base          config.Config
	settings      *SettingsService
	store         *store.Store
	logger        *slog.Logger
	notifier      *WebhookNotifier
	recordAudit   auditRecorder
	activeRestores map[string]*ActiveRestore
	mu            sync.RWMutex
	multiDBMode   bool
	databases     map[string]config.DatabaseConfig
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

type ActiveRestore struct {
	ID            string
	DatabaseID    string
	RestoreService *restore.Service
	Cancel        context.CancelFunc
	Progress      int
	Message       string
	Started       time.Time
}

type RestoreRequest struct {
	DatabaseID       string   `json:"databaseId"`
	SourceBackupID   string   `json:"sourceBackupId"`
	S3Key            string   `json:"s3Key,omitempty"`
	LocalPath        string   `json:"localPath,omitempty"`
	TargetDatabase   string   `json:"targetDatabase"`
	Clean            bool     `json:"clean"`
	CreateDB         bool     `json:"createDb"`
	IfExists         bool     `json:"ifExists"`
	NoOwner          bool     `json:"noOwner"`
	NoPrivileges     bool     `json:"noPrivileges"`
	DataOnly         bool     `json:"dataOnly"`
	SchemaOnly       bool     `json:"schemaOnly"`
	SingleTransaction bool     `json:"singleTransaction"`
	Jobs             int      `json:"jobs"`
	Schemas          []string `json:"schemas"`
	Tables           []string `json:"tables"`
	ExcludeSchemas   []string `json:"excludeSchemas"`
	ExcludeTables    []string `json:"excludeTables"`
}

func NewRestoreManager(base config.Config, settings *SettingsService, store *store.Store, logger *slog.Logger, notifier *WebhookNotifier, recorder auditRecorder) *RestoreManager {
	ctx, cancel := context.WithCancel(context.Background())

	// Check if multi-DB mode is enabled
	multiDBMode := base.MultiDatabaseMode
	databases := make(map[string]config.DatabaseConfig)

	// Load multi-DB config if available
	if multiDBMode && len(base.Databases.Databases) > 0 {
		for _, db := range base.Databases.Databases {
			databases[db.ID] = db
			logger.Info("Loaded database config for restore",
				"database_id", db.ID,
				"name", db.Name,
				"s3_prefix", db.S3Prefix,
				"filename_prefix", db.FilenamePrefix)
		}
	}

	rm := &RestoreManager{
		base:           base,
		settings:       settings,
		store:          store,
		logger:         logger,
		notifier:       notifier,
		recordAudit:    recorder,
		activeRestores: make(map[string]*ActiveRestore),
		multiDBMode:    multiDBMode,
		databases:      databases,
		ctx:            ctx,
		cancel:         cancel,
	}

	return rm
}

func (rm *RestoreManager) TriggerRestore(req *RestoreRequest, triggeredBy string) (*store.RestoreRun, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	var dbConfig config.DatabaseConfig
	var s3Prefix string
	var filenamePrefix string

	if rm.multiDBMode {
		if req.DatabaseID == "" {
			return nil, fmt.Errorf("database_id is required in multi-database mode")
		}
		db, ok := rm.databases[req.DatabaseID]
		if !ok {
			return nil, fmt.Errorf("unknown database_id %q", req.DatabaseID)
		}
		if !db.Enabled {
			return nil, fmt.Errorf("database %q is disabled", req.DatabaseID)
		}
		dbConfig = db
		s3Prefix = strings.Trim(db.GetS3Prefix(rm.base.S3.Prefix), "/")
		filenamePrefix = db.GetFilenamePrefix()
		rm.logger.Info("using multi-db config for restore",
			"database_id", req.DatabaseID,
			"s3_prefix", s3Prefix,
			"filename_prefix", filenamePrefix)
	} else {
		if req.DatabaseID != "" {
			return nil, fmt.Errorf("multi-database mode is not enabled; remove database_id")
		}
		dbConfig = config.DatabaseConfig{
			Host:     rm.base.Postgres.Host,
			Port:     rm.base.Postgres.Port,
			User:     rm.base.Postgres.Username,
			Password: rm.base.Postgres.Password,
			Database: rm.base.Postgres.Database,
		}
		s3Prefix = rm.base.S3.Prefix
		filenamePrefix = rm.base.Backup.FilenamePrefix
	}

	// Create restore run record
	restoreRun := store.RestoreRun{
		ID:             store.NewID(),
		DatabaseID:     req.DatabaseID,
		TargetDatabase: req.TargetDatabase,
		SourceBackupID: req.SourceBackupID,
		S3Key:          req.S3Key,
		LocalPath:      req.LocalPath,
		TriggeredBy:    triggeredBy,
		TriggerSource:  "dashboard",
		Status:         "running",
		Options: store.RestoreOptions{
			Clean:            req.Clean,
			CreateDB:         req.CreateDB,
			IfExists:         req.IfExists,
			NoOwner:          req.NoOwner,
			NoPrivileges:     req.NoPrivileges,
			DataOnly:         req.DataOnly,
			SchemaOnly:       req.SchemaOnly,
			SingleTransaction: req.SingleTransaction,
			Jobs:             req.Jobs,
			Schemas:          req.Schemas,
			Tables:           req.Tables,
			ExcludeSchemas:   req.ExcludeSchemas,
			ExcludeTables:    req.ExcludeTables,
		},
		StartedAt: time.Now(),
	}

	// Insert restore run
	if err := rm.store.InsertRestoreRun(rm.ctx, restoreRun); err != nil {
		return nil, fmt.Errorf("failed to insert restore run: %w", err)
	}

	// Create restore config with proper S3 prefix
	restoreConfig := &config.Config{
		Postgres: config.PostgresConfig{
			Host:     dbConfig.Host,
			Port:     dbConfig.Port,
			Username: dbConfig.User,
			Password: dbConfig.Password,
			Database: req.TargetDatabase,
		},
		S3: config.S3Config{
			Bucket:          rm.base.S3.Bucket,
			Region:          rm.base.S3.Region,
			Prefix:          s3Prefix, // Use database-specific prefix
			EndpointURL:     rm.base.S3.EndpointURL,
			AccessKeyID:     rm.base.S3.AccessKeyID,
			SecretAccessKey: rm.base.S3.SecretAccessKey,
		},
		Backup: config.BackupConfig{
			FilenamePrefix: filenamePrefix, // Use database-specific filename prefix
			Compression:    6,
		},
	}

	// Create restore service
	restoreService, err := restore.NewService(rm.ctx, *restoreConfig, rm.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create restore service: %w", err)
	}

	// Create context for this restore operation
	restoreCtx, cancelFunc := context.WithCancel(rm.ctx)

	// Track active restore
	activeRestore := &ActiveRestore{
		ID:             restoreRun.ID,
		DatabaseID:     req.DatabaseID,
		RestoreService: restoreService,
		Cancel:         cancelFunc,
		Started:        time.Now(),
	}
	rm.activeRestores[restoreRun.ID] = activeRestore

	// Start restore in background
	rm.wg.Add(1)
	go func() {
		defer rm.wg.Done()
		defer func() {
			rm.mu.Lock()
			delete(rm.activeRestores, restoreRun.ID)
			rm.mu.Unlock()
		}()

		// Perform restore
		var restoreErr error
		if req.S3Key != "" {
			// Restore from S3
			restoreReq := restore.RestoreRequest{
				S3Key:        req.S3Key,
				TargetDBName: req.TargetDatabase,
				DatabaseID:   req.DatabaseID,
				Options: restore.RestoreOptions{
					TargetDB:       req.TargetDatabase,
					Clean:          req.Clean,
					CreateDB:       req.CreateDB,
					IfExists:       req.IfExists,
					NoOwner:        req.NoOwner,
					NoPrivileges:   req.NoPrivileges,
					DataOnly:       req.DataOnly,
					SchemaOnly:     req.SchemaOnly,
					Jobs:           req.Jobs,
					Schemas:        req.Schemas,
					Tables:         req.Tables,
					ExcludeSchemas: req.ExcludeSchemas,
					ExcludeTables:  req.ExcludeTables,
					DryRun:         false,
					Verbose:        true,
				},
			}
			_, restoreErr = restoreService.RestoreFromS3(restoreCtx, restoreReq)
		} else if req.LocalPath != "" {
			// Restore from local file
			restoreOpts := restore.RestoreOptions{
				ArchivePath:    req.LocalPath,
				TargetDB:       req.TargetDatabase,
				Clean:          req.Clean,
				CreateDB:       req.CreateDB,
				IfExists:       req.IfExists,
				NoOwner:        req.NoOwner,
				NoPrivileges:   req.NoPrivileges,
				DataOnly:       req.DataOnly,
				SchemaOnly:     req.SchemaOnly,
				Jobs:           req.Jobs,
				Schemas:        req.Schemas,
				Tables:         req.Tables,
				ExcludeSchemas: req.ExcludeSchemas,
				ExcludeTables:  req.ExcludeTables,
				DryRun:         false,
				Verbose:        true,
			}
			_, restoreErr = restoreService.RestoreFromLocal(restoreCtx, req.LocalPath, restoreOpts)
		} else {
			restoreErr = fmt.Errorf("no source specified (S3 key or local path)")
		}

		// Update restore run status
		now := time.Now()
		restoreRun.FinishedAt = &now
		restoreRun.DurationMs = now.Sub(restoreRun.StartedAt).Milliseconds()

		updates := bson.M{
			"finishedAt": now,
			"durationMs": restoreRun.DurationMs,
		}

		if restoreErr != nil {
			restoreRun.Status = "failed"
			restoreRun.Error = restoreErr.Error()
			updates["status"] = "failed"
			updates["error"] = restoreErr.Error()
			log.Printf("Restore %s failed: %v", restoreRun.ID, restoreErr)
		} else {
			restoreRun.Status = "success"
			updates["status"] = "success"
			log.Printf("Restore %s completed successfully", restoreRun.ID)
		}

		// Update in database
		if updateErr := rm.store.UpdateRestoreRun(rm.ctx, restoreRun.ID, updates); updateErr != nil {
			log.Printf("Failed to update restore run %s: %v", restoreRun.ID, updateErr)
		}

		// Send webhook notification
		rm.sendWebhookNotification(context.Background(), &restoreRun)

		// Log audit event
		rm.recordAudit(context.Background(), "restore."+restoreRun.Status, triggeredBy, fmt.Sprintf("Restore %s", restoreRun.Status), map[string]any{
			"restore_id":      restoreRun.ID,
			"database_id":     restoreRun.DatabaseID,
			"target_database": restoreRun.TargetDatabase,
			"source_backup":   restoreRun.SourceBackupID,
		})
	}()

	return &restoreRun, nil
}

func (rm *RestoreManager) CancelRestore(restoreID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	activeRestore, exists := rm.activeRestores[restoreID]
	if !exists {
		return fmt.Errorf("restore %s not found or not running", restoreID)
	}

	// Cancel the restore context
	activeRestore.Cancel()

	// Update restore status in database
	now := time.Now()
	updates := bson.M{
		"status":     "cancelled",
		"finishedAt": now,
		"error":      "Cancelled by user",
	}

	if err := rm.store.UpdateRestoreRun(rm.ctx, restoreID, updates); err != nil {
		return fmt.Errorf("failed to update restore status: %w", err)
	}

	rm.logger.Info("Restore cancelled", "restore_id", restoreID)
	return nil
}

func (rm *RestoreManager) GetActiveRestores() map[string]*ActiveRestore {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]*ActiveRestore)
	for k, v := range rm.activeRestores {
		result[k] = v
	}
	return result
}

func (rm *RestoreManager) GetRestoreRuns(filter store.RestoreRunFilter, limit, offset int) ([]store.RestoreRun, int64, error) {
	return rm.store.ListRestoreRunsWithFilter(rm.ctx, filter, limit, offset)
}

func (rm *RestoreManager) GetRestoreRun(restoreID string) (*store.RestoreRun, error) {
	return rm.store.GetRestoreRun(rm.ctx, restoreID)
}

func (rm *RestoreManager) sendWebhookNotification(ctx context.Context, restoreRun *store.RestoreRun) {
	// Get settings for webhook configuration
	settings, err := rm.settings.GetActive(ctx)
	if err != nil {
		rm.logger.Error("failed to get settings for webhook", "error", err)
		return
	}

	// Send notification if restore failed
	if restoreRun.Status == "failed" && restoreRun.Error != "" {
		metadata := map[string]any{
			"restore_id":      restoreRun.ID,
			"database_id":     restoreRun.DatabaseID,
			"target_database": restoreRun.TargetDatabase,
			"source_backup":   restoreRun.SourceBackupID,
			"started_at":      restoreRun.StartedAt,
			"finished_at":     restoreRun.FinishedAt,
			"duration_ms":     restoreRun.DurationMs,
		}
		rm.notifier.NotifyFailure(ctx, settings, "restore.failed", "system", restoreRun.Error, metadata)
	}
}

func (rm *RestoreManager) Wait(ctx context.Context) error {
	_ = ctx
	rm.wg.Wait()
	return nil
}

func (rm *RestoreManager) ListAvailableBackups(databaseID string) ([]map[string]interface{}, error) {
	var s3Prefix string

	if rm.multiDBMode {
		if databaseID == "" {
			return nil, fmt.Errorf("database_id is required in multi-database mode")
		}
		db, ok := rm.databases[databaseID]
		if !ok {
			return nil, fmt.Errorf("unknown database_id %q", databaseID)
		}
		s3Prefix = strings.Trim(db.GetS3Prefix(rm.base.S3.Prefix), "/")
	} else {
		if databaseID != "" {
			return nil, fmt.Errorf("multi-database mode is not enabled; remove database_id")
		}
		s3Prefix = rm.base.S3.Prefix
	}

	// Create a temporary config with the correct S3 prefix
	cfg := &config.Config{
		S3: config.S3Config{
			Bucket:          rm.base.S3.Bucket,
			Region:          rm.base.S3.Region,
			Prefix:          s3Prefix,
			EndpointURL:     rm.base.S3.EndpointURL,
			AccessKeyID:     rm.base.S3.AccessKeyID,
			SecretAccessKey: rm.base.S3.SecretAccessKey,
		},
	}

	// Use restore service to list backups
	restoreService, err := restore.NewService(rm.ctx, *cfg, rm.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create restore service: %w", err)
	}
	backups, err := restoreService.ListAvailableBackups(rm.ctx, databaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list backups for database %s: %w", databaseID, err)
	}

	// Convert to generic map for API response
	var result []map[string]interface{}
	for _, backup := range backups {
		result = append(result, map[string]interface{}{
			"key":      backup.Key,
			"size":     backup.Size,
			"modified": backup.LastModified,
			"database": databaseID,
			"prefix":   s3Prefix,
		})
	}

	rm.logger.Info("Listed available backups",
		"database_id", databaseID,
		"s3_prefix", s3Prefix,
		"count", len(result))

	return result, nil
}

func (rm *RestoreManager) GetDatabases() map[string]config.DatabaseConfig {
	return rm.databases
}

func (rm *RestoreManager) Stop() {
	rm.cancel()
	rm.wg.Wait()
}