package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/logging"
	"github.com/neelgai/postgres-backup/internal/service"
)

const (
	exitOK      = 0
	exitRuntime = 1
	exitConfig  = 2
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath  string
		showVersion bool
		parallel    bool
		databaseID  string
	)

	flag.StringVar(&configPath, "config", "", "Path to a KEY=VALUE environment file")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&parallel, "parallel", false, "Run backups in parallel (multi-database mode)")
	flag.StringVar(&databaseID, "database", "", "Backup specific database by ID (multi-database mode)")
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		return exitOK
	}

	if configPath != "" {
		if err := config.LoadEnvFile(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "load config file: %v\n", err)
			return exitConfig
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load configuration: %v\n", err)
		return exitConfig
	}

	logger, err := logging.New(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build logger: %v\n", err)
		return exitConfig
	}

	if configPath != "" {
		logger.Info("loaded environment file", "path", configPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Handle multi-database mode
	if cfg.MultiDatabaseMode {
		logger.Info("multi-database mode enabled", "config_file", os.Getenv("DATABASE_CONFIG_FILE"))

		multiSvc, err := service.NewMultiDatabaseService(ctx, cfg, logger)
		if err != nil {
			logger.Error("initialize multi-database backup service", "error", err)
			return exitConfig
		}

		// Backup specific database if ID provided
		if databaseID != "" {
			logger.Info("backing up specific database", "database_id", databaseID)
			result, err := multiSvc.BackupSpecificDatabase(ctx, databaseID)
			if err != nil {
				logger.Error("backup failed", "database_id", databaseID, "error", err)
				return exitRuntime
			}
			if result.Error != nil {
				logger.Error("backup failed", "database_id", databaseID, "error", result.Error)
				return exitRuntime
			}
			logger.Info("database backup completed successfully",
				"database_id", databaseID,
				"duration", result.Duration,
				"s3_uri", result.S3URI)
			return exitOK
		}

		// Run all enabled databases
		result, err := multiSvc.Run(ctx, parallel)
		if err != nil {
			logger.Error("multi-database backup failed", "error", err)
			return exitRuntime
		}

		logger.Info("multi-database backup completed",
			"total_databases", len(result.Results),
			"successful", result.Successful,
			"failed", result.Failed,
			"total_duration", result.TotalTime)

		if result.Failed > 0 {
			logger.Error("some backups failed", "failed_count", result.Failed)
			return exitRuntime
		}

		return exitOK
	}

	// Single database mode (backward compatibility)
	logger.Info("configuration ready", "database", cfg.Postgres.Database, "bucket", cfg.S3.Bucket, "output_dir", cfg.Backup.OutputDir)
	logger.Debug("configuration details", cfg.LogFields()...)

	svc, err := service.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("initialize backup service", "error", err)
		return exitConfig
	}

	if err := svc.Run(ctx); err != nil {
		logger.Error("backup job failed", "error", err)
		return exitRuntime
	}

	logger.Info("backup job completed successfully")
	return exitOK
}
