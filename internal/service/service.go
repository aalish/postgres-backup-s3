package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/neelgai/postgres-backup/internal/backup"
	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/storage"
)

type Service struct {
	dumper   *backup.Dumper
	uploader *storage.S3Uploader
	logger   *slog.Logger
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Service, error) {
	uploader, err := storage.NewS3Uploader(ctx, cfg.S3, cfg.Retry, logger)
	if err != nil {
		return nil, err
	}

	return &Service{
		dumper:   backup.NewDumper(cfg.Postgres, cfg.Backup, logger),
		uploader: uploader,
		logger:   logger,
	}, nil
}

func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("backup workflow started")

	createStarted := time.Now()
	s.logger.Info("stage started", "stage", "create_dump", "step", "1/4")
	archive, err := s.dumper.Create(ctx)
	if err != nil {
		return err
	}
	s.logger.Info("stage completed", "stage", "create_dump", "step", "1/4", "duration", time.Since(createStarted).String(), "archive_path", archive.Path)

	validateStarted := time.Now()
	s.logger.Info("stage started", "stage", "validate_dump", "step", "2/4", "archive_path", archive.Path)
	if err := s.dumper.Validate(ctx, archive); err != nil {
		return err
	}
	s.logger.Info("stage completed", "stage", "validate_dump", "step", "2/4", "duration", time.Since(validateStarted).String(), "archive_path", archive.Path)

	uploadStarted := time.Now()
	s.logger.Info("stage started", "stage", "upload_s3", "step", "3/4", "archive_path", archive.Path)
	s3URI, err := s.uploader.Upload(ctx, archive)
	if err != nil {
		return err
	}
	s.logger.Info("stage completed", "stage", "upload_s3", "step", "3/4", "duration", time.Since(uploadStarted).String(), "s3_uri", s3URI)

	cleanupStarted := time.Now()
	s.logger.Info("stage started", "stage", "cleanup_local_archive", "step", "4/4", "archive_path", archive.Path)
	if err := os.Remove(archive.Path); err != nil {
		return fmt.Errorf("remove local archive after successful upload: %w", err)
	}

	s.logger.Info("stage completed", "stage", "cleanup_local_archive", "step", "4/4", "duration", time.Since(cleanupStarted).String(), "path", archive.Path)
	s.logger.Info("local archive deleted after successful upload", "path", archive.Path, "s3_uri", s3URI)
	return nil
}
