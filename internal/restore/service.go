package restore

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/storage"
)

// Service handles the complete restore workflow
type Service struct {
	config       config.Config
	s3Client     *s3.Client
	restorer     *Restorer
	logger       *slog.Logger
	downloadDir  string
}

// RestoreRequest represents a request to restore a database
type RestoreRequest struct {
	// Source backup information
	S3Key       string // S3 key of the backup to restore
	S3URI       string // Alternative: full S3 URI (s3://bucket/key)
	BackupID    string // Alternative: backup ID from database

	// Target database information
	DatabaseID   string // Database configuration ID (for multi-db mode)
	TargetDBName string // Target database name (overrides config)

	// Restore options
	Options RestoreOptions

	// Download options
	KeepDownload bool // Keep downloaded backup file after restore
}

// RestoreResponse contains the complete restore operation result
type RestoreResponse struct {
	Request      RestoreRequest
	DownloadPath string
	DownloadSize int64
	DownloadTime time.Duration
	RestoreResult *RestoreResult
	TotalTime    time.Duration
	Success      bool
	Error        error
}

// NewService creates a new restore service
func NewService(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Service, error) {
	// Initialize S3 client
	uploader, err := storage.NewS3Uploader(ctx, cfg.S3, cfg.Retry, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Create download directory
	downloadDir := filepath.Join(cfg.Backup.OutputDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	// Use default postgres config (can be overridden per restore request)
	restorer := NewRestorer(cfg.Postgres, downloadDir, logger)

	return &Service{
		config:      cfg,
		s3Client:    uploader.Client(),
		restorer:    restorer,
		logger:      logger,
		downloadDir: downloadDir,
	}, nil
}

// NewServiceForDatabase creates a restore service for a specific database (multi-db mode)
func NewServiceForDatabase(ctx context.Context, cfg config.Config, databaseID string, logger *slog.Logger) (*Service, error) {
	if !cfg.MultiDatabaseMode {
		return nil, fmt.Errorf("multi-database mode is not enabled")
	}

	// Get database config
	db, err := cfg.Databases.GetByID(databaseID)
	if err != nil {
		return nil, fmt.Errorf("database configuration not found: %w", err)
	}

	// Initialize S3 client
	uploader, err := storage.NewS3Uploader(ctx, cfg.S3, cfg.Retry, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Create download directory
	downloadDir := filepath.Join(cfg.Backup.OutputDir, "downloads", databaseID)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	// Create database-specific postgres config
	postgresConfig := config.PostgresConfig{
		Host:          db.Host,
		Port:          db.Port,
		Username:      db.User,
		Password:      db.Password,
		Database:      db.Database,
		PGDumpPath:    cfg.Postgres.PGDumpPath,
		PGRestorePath: cfg.Postgres.PGRestorePath,
	}

	restorer := NewRestorer(postgresConfig, downloadDir, logger)

	return &Service{
		config:      cfg,
		s3Client:    uploader.Client(),
		restorer:    restorer,
		logger:      logger,
		downloadDir: downloadDir,
	}, nil
}

// RestoreFromS3 downloads a backup from S3 and restores it
func (s *Service) RestoreFromS3(ctx context.Context, request RestoreRequest) (*RestoreResponse, error) {
	startTime := time.Now()
	response := &RestoreResponse{
		Request: request,
		Success: false,
	}

	s.logger.Info("starting restore workflow", "s3_key", request.S3Key, "target_db", request.TargetDBName)

	// Parse S3 key from URI if provided
	if request.S3URI != "" && request.S3Key == "" {
		key, err := s.parseS3URI(request.S3URI)
		if err != nil {
			response.Error = fmt.Errorf("invalid S3 URI: %w", err)
			return response, response.Error
		}
		request.S3Key = key
	}

	// Step 1: Download backup from S3
	downloadStart := time.Now()
	s.logger.Info("downloading backup from S3", "bucket", s.config.S3.Bucket, "key", request.S3Key)

	downloadPath, downloadSize, err := s.downloadFromS3(ctx, request.S3Key)
	if err != nil {
		response.Error = fmt.Errorf("failed to download backup: %w", err)
		response.TotalTime = time.Since(startTime)
		return response, response.Error
	}

	response.DownloadPath = downloadPath
	response.DownloadSize = downloadSize
	response.DownloadTime = time.Since(downloadStart)

	s.logger.Info("backup downloaded successfully",
		"path", downloadPath,
		"size", downloadSize,
		"duration", response.DownloadTime)

	// Step 2: Prepare target database
	targetDB := request.TargetDBName
	if targetDB == "" {
		if s.config.MultiDatabaseMode && request.DatabaseID != "" {
			db, err := s.config.Databases.GetByID(request.DatabaseID)
			if err != nil {
				response.Error = fmt.Errorf("database configuration not found: %w", err)
				response.TotalTime = time.Since(startTime)
				s.cleanupDownload(downloadPath, request.KeepDownload)
				return response, response.Error
			}
			targetDB = db.Database
		} else {
			targetDB = s.config.Postgres.Database
		}
	}

	// Step 3: Handle database preparation based on options
	if request.Options.Clean && request.Options.CreateDB {
		s.logger.Info("recreating database for clean restore", "database", targetDB)
		if err := s.restorer.DropDatabaseIfExists(ctx, targetDB); err != nil {
			response.Error = fmt.Errorf("failed to drop existing database: %w", err)
			response.TotalTime = time.Since(startTime)
			s.cleanupDownload(downloadPath, request.KeepDownload)
			return response, response.Error
		}
		if err := s.restorer.CreateDatabaseIfNotExists(ctx, targetDB); err != nil {
			response.Error = fmt.Errorf("failed to create database: %w", err)
			response.TotalTime = time.Since(startTime)
			s.cleanupDownload(downloadPath, request.KeepDownload)
			return response, response.Error
		}
	} else if request.Options.CreateDB {
		if err := s.restorer.CreateDatabaseIfNotExists(ctx, targetDB); err != nil {
			response.Error = fmt.Errorf("failed to create database: %w", err)
			response.TotalTime = time.Since(startTime)
			s.cleanupDownload(downloadPath, request.KeepDownload)
			return response, response.Error
		}
	}

	// Step 4: Restore the database
	request.Options.ArchivePath = downloadPath
	request.Options.TargetDB = targetDB

	restoreResult, err := s.restorer.Restore(ctx, request.Options)
	response.RestoreResult = restoreResult

	if err != nil {
		response.Error = fmt.Errorf("restore failed: %w", err)
		response.TotalTime = time.Since(startTime)
		s.cleanupDownload(downloadPath, request.KeepDownload)
		return response, response.Error
	}

	// Step 5: Cleanup downloaded file
	s.cleanupDownload(downloadPath, request.KeepDownload)

	response.Success = true
	response.TotalTime = time.Since(startTime)

	s.logger.Info("restore workflow completed successfully",
		"database", targetDB,
		"total_duration", response.TotalTime,
		"download_time", response.DownloadTime,
		"restore_time", restoreResult.Duration)

	return response, nil
}

// ListAvailableBackups lists all available backups in S3
func (s *Service) ListAvailableBackups(ctx context.Context, databaseID string) ([]storage.ObjectInfo, error) {
	prefix := s.config.S3.Prefix

	// If database ID is provided, filter by database-specific prefix
	if databaseID != "" && s.config.MultiDatabaseMode {
		db, err := s.config.Databases.GetByID(databaseID)
		if err != nil {
			return nil, fmt.Errorf("database not found: %w", err)
		}
		prefix = db.GetS3Prefix(s.config.S3.Prefix)
	}

	// Create S3 uploader just to use its list functionality
	uploader, err := storage.NewS3Uploader(ctx, s.config.S3, s.config.Retry, s.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Override prefix if database-specific
	if prefix != s.config.S3.Prefix {
		// Need to list with specific prefix
		input := &s3.ListObjectsV2Input{
			Bucket: aws.String(s.config.S3.Bucket),
			Prefix: aws.String(prefix + "/"),
		}

		var objects []storage.ObjectInfo
		paginator := s3.NewListObjectsV2Paginator(s.s3Client, input)

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to list S3 objects: %w", err)
			}

			for _, obj := range page.Contents {
				key := aws.ToString(obj.Key)
				objects = append(objects, storage.ObjectInfo{
					Key:          key,
					Filename:     filepath.Base(key),
					Size:         aws.ToInt64(obj.Size),
					LastModified: aws.ToTime(obj.LastModified),
					S3URI:        fmt.Sprintf("s3://%s/%s", s.config.S3.Bucket, key),
				})
			}
		}

		return objects, nil
	}

	// Use default listing
	return uploader.ListObjects(ctx)
}

// RestoreFromLocal restores from a local backup file
func (s *Service) RestoreFromLocal(ctx context.Context, archivePath string, options RestoreOptions) (*RestoreResult, error) {
	s.logger.Info("starting local restore", "archive", archivePath, "target_db", options.TargetDB)

	// Validate the archive exists
	if _, err := os.Stat(archivePath); err != nil {
		return nil, fmt.Errorf("archive file not found: %w", err)
	}

	// Prepare target database if needed
	if options.CreateDB && options.TargetDB != "" {
		if options.Clean {
			if err := s.restorer.DropDatabaseIfExists(ctx, options.TargetDB); err != nil {
				return nil, fmt.Errorf("failed to drop existing database: %w", err)
			}
		}
		if err := s.restorer.CreateDatabaseIfNotExists(ctx, options.TargetDB); err != nil {
			return nil, fmt.Errorf("failed to create database: %w", err)
		}
	}

	options.ArchivePath = archivePath
	return s.restorer.Restore(ctx, options)
}

// downloadFromS3 downloads a file from S3
func (s *Service) downloadFromS3(ctx context.Context, key string) (string, int64, error) {
	// Generate local filename
	filename := filepath.Base(key)
	downloadPath := filepath.Join(s.downloadDir, filename)

	// Create or truncate the file
	file, err := os.Create(downloadPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create download file: %w", err)
	}
	defer file.Close()

	// Download from S3
	result, err := s.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.S3.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		os.Remove(downloadPath)
		return "", 0, fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer result.Body.Close()

	// Copy to file
	written, err := io.Copy(file, result.Body)
	if err != nil {
		os.Remove(downloadPath)
		return "", 0, fmt.Errorf("failed to write download file: %w", err)
	}

	return downloadPath, written, nil
}

// parseS3URI extracts the key from an S3 URI
func (s *Service) parseS3URI(uri string) (string, error) {
	if !strings.HasPrefix(uri, "s3://") {
		return "", fmt.Errorf("URI must start with s3://")
	}

	trimmed := strings.TrimPrefix(uri, "s3://")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid S3 URI format")
	}

	bucket := parts[0]
	key := parts[1]

	// Verify bucket matches configuration
	if bucket != s.config.S3.Bucket {
		return "", fmt.Errorf("bucket mismatch: expected %s, got %s", s.config.S3.Bucket, bucket)
	}

	return key, nil
}

// cleanupDownload removes the downloaded file unless keep is true
func (s *Service) cleanupDownload(path string, keep bool) {
	if keep {
		s.logger.Info("keeping downloaded backup file", "path", path)
		return
	}

	if err := os.Remove(path); err != nil {
		s.logger.Warn("failed to remove downloaded file", "path", path, "error", err)
	} else {
		s.logger.Info("removed downloaded backup file", "path", path)
	}
}