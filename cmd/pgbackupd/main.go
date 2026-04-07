package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/dashboard"
	"github.com/neelgai/postgres-backup/internal/logging"
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

	cfg, err := config.LoadDashboard()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load dashboard configuration: %v\n", err)
		return exitConfig
	}

	logger, err := logging.New(cfg.Base.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build logger: %v\n", err)
		return exitConfig
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := dashboard.New(ctx, cfg, logger, version)
	if err != nil {
		logger.Error("initialize dashboard", "error", err)
		return exitConfig
	}

	go func() {
		<-ctx.Done()
		if err := app.Shutdown(context.Background()); err != nil {
			logger.Error("shutdown dashboard", "error", err)
		}
	}()

	if err := app.Start(context.Background()); err != nil {
		logger.Error("dashboard server failed", "error", err)
		return exitRuntime
	}

	return exitOK
}
