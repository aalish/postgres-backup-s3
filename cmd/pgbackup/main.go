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
	)

	flag.StringVar(&configPath, "config", "", "Path to a KEY=VALUE environment file")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
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
	logger.Info("configuration ready", "database", cfg.Postgres.Database, "bucket", cfg.S3.Bucket, "output_dir", cfg.Backup.OutputDir)
	logger.Debug("configuration details", cfg.LogFields()...)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
