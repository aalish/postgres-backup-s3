package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/neelgai/postgres-backup/internal/backup"
	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/storage"
)

// MultiDatabaseService handles backups for multiple databases
type MultiDatabaseService struct {
	config   config.Config
	uploader *storage.S3Uploader
	logger   *slog.Logger
}

// DatabaseBackupResult represents the result of a single database backup
type DatabaseBackupResult struct {
	DatabaseID      string
	DatabaseName    string
	ArchiveFilename string
	ArchivePath     string
	ArchiveSize     int64
	S3URI           string
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	Error           error
}

// MultiDatabaseResult contains results for all database backups
type MultiDatabaseResult struct {
	Results    []DatabaseBackupResult
	TotalTime  time.Duration
	Successful int
	Failed     int
}

// NewMultiDatabaseService creates a new multi-database backup service
func NewMultiDatabaseService(ctx context.Context, cfg config.Config, logger *slog.Logger) (*MultiDatabaseService, error) {
	uploader, err := storage.NewS3Uploader(ctx, cfg.S3, cfg.Retry, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 uploader: %w", err)
	}

	return &MultiDatabaseService{
		config:   cfg,
		uploader: uploader,
		logger:   logger,
	}, nil
}

// Run executes backups for all enabled databases
func (mds *MultiDatabaseService) Run(ctx context.Context, parallel bool) (*MultiDatabaseResult, error) {
	enabledDatabases := mds.config.Databases.GetEnabled()
	if len(enabledDatabases) == 0 {
		return nil, fmt.Errorf("no enabled databases found")
	}

	mds.logger.Info("starting multi-database backup", "database_count", len(enabledDatabases), "parallel", parallel)

	startTime := time.Now()
	var results []DatabaseBackupResult

	if parallel {
		results = mds.runParallel(ctx, enabledDatabases)
	} else {
		results = mds.runSequential(ctx, enabledDatabases)
	}

	// Calculate statistics
	totalTime := time.Since(startTime)
	successful := 0
	failed := 0
	for _, result := range results {
		if result.Error == nil {
			successful++
		} else {
			failed++
		}
	}

	mds.logger.Info("multi-database backup completed",
		"total_databases", len(results),
		"successful", successful,
		"failed", failed,
		"total_duration", totalTime.String())

	return &MultiDatabaseResult{
		Results:    results,
		TotalTime:  totalTime,
		Successful: successful,
		Failed:     failed,
	}, nil
}

// runSequential executes database backups one after another
func (mds *MultiDatabaseService) runSequential(ctx context.Context, databases []config.DatabaseConfig) []DatabaseBackupResult {
	results := make([]DatabaseBackupResult, 0, len(databases))

	for _, db := range databases {
		result := mds.backupDatabase(ctx, db)
		results = append(results, result)

		// Continue even if one database fails
		if result.Error != nil {
			mds.logger.Error("database backup failed", "database", db.Name, "error", result.Error)
		}
	}

	return results
}

// runParallel executes database backups concurrently
func (mds *MultiDatabaseService) runParallel(ctx context.Context, databases []config.DatabaseConfig) []DatabaseBackupResult {
	var wg sync.WaitGroup
	resultsChan := make(chan DatabaseBackupResult, len(databases))

	for _, db := range databases {
		wg.Add(1)
		go func(database config.DatabaseConfig) {
			defer wg.Done()
			result := mds.backupDatabase(ctx, database)
			resultsChan <- result

			if result.Error != nil {
				mds.logger.Error("database backup failed", "database", database.Name, "error", result.Error)
			}
		}(db)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	var results []DatabaseBackupResult
	for result := range resultsChan {
		results = append(results, result)
	}

	return results
}

// backupDatabase performs backup for a single database
func (mds *MultiDatabaseService) backupDatabase(ctx context.Context, db config.DatabaseConfig) DatabaseBackupResult {
	startTime := time.Now()
	result := DatabaseBackupResult{
		DatabaseID:   db.ID,
		DatabaseName: db.Name,
		StartTime:    startTime,
	}

	mds.logger.Info("starting database backup", "database_id", db.ID, "database_name", db.Name)

	// Create database-specific backup config
	postgresConfig := config.PostgresConfig{
		Host:          db.Host,
		Port:          db.Port,
		Username:      db.User,
		Password:      db.Password,
		Database:      db.Database,
		PGDumpPath:    mds.config.Postgres.PGDumpPath,
		PGRestorePath: mds.config.Postgres.PGRestorePath,
	}

	backupConfig := config.BackupConfig{
		OutputDir:      mds.config.Backup.OutputDir,
		FilenamePrefix: db.GetFilenamePrefix(),
		Compression:    db.CompressionLevel,
	}

	// Override compression if not specified
	if backupConfig.Compression == 0 {
		backupConfig.Compression = mds.config.Backup.Compression
	}

	// Create dumper for this database
	dumper := backup.NewDumper(postgresConfig, backupConfig, mds.logger)

	// Stage 1: Create dump
	archive, err := dumper.Create(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to create dump: %w", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		return result
	}

	result.ArchiveFilename = archive.Filename
	result.ArchivePath = archive.Path
	result.ArchiveSize = archive.Size

	// Stage 2: Validate dump
	if err := dumper.Validate(ctx, archive); err != nil {
		result.Error = fmt.Errorf("failed to validate dump: %w", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		// Try to clean up the invalid dump
		os.Remove(archive.Path)
		return result
	}

	// Stage 3: Upload to S3 with database-specific prefix
	s3Key := fmt.Sprintf("%s/%s", db.GetS3Prefix(mds.config.S3.Prefix), archive.Filename)
	s3URI, err := mds.uploader.UploadWithKey(ctx, archive, s3Key)
	if err != nil {
		result.Error = fmt.Errorf("failed to upload to S3: %w", err)
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		// Try to clean up the local file
		os.Remove(archive.Path)
		return result
	}

	result.S3URI = s3URI

	// Stage 4: Cleanup local archive
	if err := os.Remove(archive.Path); err != nil {
		mds.logger.Warn("failed to remove local archive after successful upload",
			"database", db.Name,
			"path", archive.Path,
			"error", err)
		// Don't fail the backup if cleanup fails
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)

	mds.logger.Info("database backup completed successfully",
		"database_id", db.ID,
		"database_name", db.Name,
		"duration", result.Duration.String(),
		"s3_uri", s3URI,
		"archive_size", result.ArchiveSize)

	return result
}

// BackupSpecificDatabase backs up a specific database by ID
func (mds *MultiDatabaseService) BackupSpecificDatabase(ctx context.Context, databaseID string) (*DatabaseBackupResult, error) {
	db, err := mds.config.Databases.GetByID(databaseID)
	if err != nil {
		return nil, fmt.Errorf("database not found: %w", err)
	}

	if !db.Enabled {
		return nil, fmt.Errorf("database %s is not enabled", databaseID)
	}

	result := mds.backupDatabase(ctx, *db)
	return &result, nil
}