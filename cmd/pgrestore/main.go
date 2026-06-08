package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/logging"
	"github.com/neelgai/postgres-backup/internal/restore"
)

const (
	exitCodeSuccess       = 0
	exitCodeRuntimeError  = 1
	exitCodeConfigError   = 2
	exitCodeUserCancelled = 3
)

func main() {
	// Define command-line flags
	var (
		envFile       = flag.String("env", "", "Path to .env file (optional)")
		s3URI         = flag.String("s3", "", "S3 URI of backup to restore (s3://bucket/key)")
		s3Key         = flag.String("key", "", "S3 key of backup to restore")
		localPath     = flag.String("local", "", "Path to local backup file to restore")
		targetDB      = flag.String("target-db", "", "Target database name (defaults to original)")
		databaseID    = flag.String("database-id", "", "Database configuration ID (multi-db mode)")

		// Restore options
		clean          = flag.Bool("clean", false, "Drop database objects before recreating them")
		createDB       = flag.Bool("create-db", false, "Create the database before restoring")
		ifExists       = flag.Bool("if-exists", false, "Use IF EXISTS when dropping objects")
		noOwner        = flag.Bool("no-owner", false, "Do not restore ownership")
		noPrivileges   = flag.Bool("no-privileges", false, "Do not restore privileges")
		dataOnly       = flag.Bool("data-only", false, "Restore only data, not schema")
		schemaOnly     = flag.Bool("schema-only", false, "Restore only schema, not data")
		singleTx       = flag.Bool("single-transaction", false, "Restore as a single transaction")
		jobs           = flag.Int("jobs", 1, "Number of parallel jobs for restore")

		// Selective restore
		schemas        = flag.String("schemas", "", "Comma-separated list of schemas to restore")
		tables         = flag.String("tables", "", "Comma-separated list of tables to restore")
		excludeSchemas = flag.String("exclude-schemas", "", "Comma-separated schemas to exclude")
		excludeTables  = flag.String("exclude-tables", "", "Comma-separated tables to exclude")

		// Other options
		listBackups   = flag.Bool("list", false, "List available backups")
		listContents  = flag.Bool("list-contents", false, "List contents of backup archive")
		keepDownload  = flag.Bool("keep-download", false, "Keep downloaded backup file")
		force         = flag.Bool("force", false, "Force restore even with warnings")
		skipConfirm   = flag.Bool("yes", false, "Skip confirmation prompt")
		dryRun        = flag.Bool("dry-run", false, "Show what would be restored without doing it")
		verbose       = flag.Bool("verbose", false, "Verbose output")
		help          = flag.Bool("help", false, "Show help message")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "PostgreSQL Restore Tool (pgrestore)\n\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [OPTIONS]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # List available backups\n")
		fmt.Fprintf(os.Stderr, "  %s --list\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Restore from S3 backup\n")
		fmt.Fprintf(os.Stderr, "  %s --s3 s3://mybucket/backups/mydb_20240101_120000.dump\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Restore from local file with clean option\n")
		fmt.Fprintf(os.Stderr, "  %s --local /path/to/backup.dump --clean --create-db\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Restore specific schemas\n")
		fmt.Fprintf(os.Stderr, "  %s --local backup.dump --schemas public,app_schema\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Dry run to see what would be restored\n")
		fmt.Fprintf(os.Stderr, "  %s --local backup.dump --dry-run --verbose\n\n", os.Args[0])
	}

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(exitCodeSuccess)
	}

	// Load environment file if specified
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load env file: %v\n", err)
			os.Exit(exitCodeConfigError)
		}
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(exitCodeConfigError)
	}

	// Initialize logger
	logger, err := logging.New(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(exitCodeConfigError)
	}

	ctx := context.Background()

	// Create restore service
	var service *restore.Service
	if *databaseID != "" && cfg.MultiDatabaseMode {
		service, err = restore.NewServiceForDatabase(ctx, cfg, *databaseID, logger)
	} else {
		service, err = restore.NewService(ctx, cfg, logger)
	}

	if err != nil {
		logger.Error("failed to create restore service", "error", err)
		os.Exit(exitCodeConfigError)
	}

	// Handle list backups
	if *listBackups {
		if err := listAvailableBackups(ctx, service, *databaseID); err != nil {
			logger.Error("failed to list backups", "error", err)
			os.Exit(exitCodeRuntimeError)
		}
		os.Exit(exitCodeSuccess)
	}

	// Determine restore source
	var restoreSource string
	if *s3URI != "" {
		restoreSource = "s3"
	} else if *s3Key != "" {
		restoreSource = "s3"
		*s3URI = fmt.Sprintf("s3://%s/%s", cfg.S3.Bucket, *s3Key)
	} else if *localPath != "" {
		restoreSource = "local"
	} else {
		fmt.Fprintf(os.Stderr, "Error: Must specify either --s3, --key, or --local\n\n")
		flag.Usage()
		os.Exit(exitCodeConfigError)
	}

	// Handle list contents
	if *listContents {
		if restoreSource != "local" {
			fmt.Fprintf(os.Stderr, "Error: --list-contents only works with --local\n")
			os.Exit(exitCodeConfigError)
		}
		if err := listArchiveContents(ctx, cfg, *localPath); err != nil {
			logger.Error("failed to list archive contents", "error", err)
			os.Exit(exitCodeRuntimeError)
		}
		os.Exit(exitCodeSuccess)
	}

	// Build restore options
	options := restore.RestoreOptions{
		TargetDB:         *targetDB,
		Clean:            *clean,
		CreateDB:         *createDB,
		IfExists:         *ifExists,
		NoOwner:          *noOwner,
		NoPrivileges:     *noPrivileges,
		DataOnly:         *dataOnly,
		SchemaOnly:       *schemaOnly,
		SingleTransction: *singleTx,
		Jobs:             *jobs,
		DryRun:           *dryRun,
		Verbose:          *verbose,
		ForceRestore:     *force,
	}

	// Parse selective restore options
	if *schemas != "" {
		options.Schemas = strings.Split(*schemas, ",")
	}
	if *tables != "" {
		options.Tables = strings.Split(*tables, ",")
	}
	if *excludeSchemas != "" {
		options.ExcludeSchemas = strings.Split(*excludeSchemas, ",")
	}
	if *excludeTables != "" {
		options.ExcludeTables = strings.Split(*excludeTables, ",")
	}

	// Confirm with user unless skipped
	if !*skipConfirm && !*dryRun {
		if !confirmRestore(restoreSource, *s3URI, *localPath, *targetDB, *clean) {
			fmt.Println("Restore cancelled by user")
			os.Exit(exitCodeUserCancelled)
		}
	}

	// Perform restore
	logger.Info("starting restore operation", "source", restoreSource, "target_db", *targetDB)

	var restoreErr error
	if restoreSource == "s3" {
		request := restore.RestoreRequest{
			S3URI:        *s3URI,
			DatabaseID:   *databaseID,
			TargetDBName: *targetDB,
			Options:      options,
			KeepDownload: *keepDownload,
		}

		response, err := service.RestoreFromS3(ctx, request)
		if err != nil {
			restoreErr = err
		} else if !response.Success {
			restoreErr = response.Error
		} else {
			logger.Info("restore completed successfully",
				"database", response.RestoreResult.DatabaseName,
				"total_time", response.TotalTime,
				"download_time", response.DownloadTime,
				"restore_time", response.RestoreResult.Duration)

			if len(response.RestoreResult.Warnings) > 0 {
				logger.Warn("restore completed with warnings",
					"warnings", response.RestoreResult.Warnings)
			}
		}
	} else {
		// Local restore
		result, err := service.RestoreFromLocal(ctx, *localPath, options)
		if err != nil {
			restoreErr = err
		} else {
			logger.Info("restore completed successfully",
				"database", result.DatabaseName,
				"duration", result.Duration)

			if len(result.Warnings) > 0 {
				logger.Warn("restore completed with warnings",
					"warnings", result.Warnings)
			}
		}
	}

	if restoreErr != nil {
		logger.Error("restore failed", "error", restoreErr)
		fmt.Fprintf(os.Stderr, "Restore failed: %v\n", restoreErr)
		os.Exit(exitCodeRuntimeError)
	}

	logger.Info("restore workflow completed successfully")
	fmt.Println("Restore completed successfully")
	os.Exit(exitCodeSuccess)
}

// confirmRestore prompts the user for confirmation
func confirmRestore(source, s3URI, localPath, targetDB string, clean bool) bool {
	fmt.Println("\n=== RESTORE CONFIRMATION ===")
	fmt.Printf("Source: %s\n", source)

	if source == "s3" {
		fmt.Printf("S3 URI: %s\n", s3URI)
	} else {
		fmt.Printf("Local file: %s\n", localPath)
	}

	if targetDB != "" {
		fmt.Printf("Target database: %s\n", targetDB)
	} else {
		fmt.Println("Target database: (using original)")
	}

	if clean {
		fmt.Println("WARNING: --clean will DROP existing database objects!")
	}

	fmt.Print("\nDo you want to proceed with the restore? (yes/no): ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "yes" || response == "y"
}

// listAvailableBackups lists all available backups
func listAvailableBackups(ctx context.Context, service *restore.Service, databaseID string) error {
	backups, err := service.ListAvailableBackups(ctx, databaseID)
	if err != nil {
		return err
	}

	if len(backups) == 0 {
		fmt.Println("No backups found")
		return nil
	}

	fmt.Printf("\nAvailable backups (%d total):\n\n", len(backups))
	fmt.Printf("%-60s %-20s %-15s %s\n", "FILENAME", "MODIFIED", "SIZE", "S3 URI")
	fmt.Println(strings.Repeat("-", 120))

	for _, backup := range backups {
		size := formatSize(backup.Size)
		modified := backup.LastModified.Format("2006-01-02 15:04:05")
		fmt.Printf("%-60s %-20s %-15s %s\n",
			truncateString(backup.Filename, 60),
			modified,
			size,
			backup.S3URI)
	}

	return nil
}

// listArchiveContents lists the contents of a backup archive
func listArchiveContents(ctx context.Context, cfg config.Config, archivePath string) error {
	restorer := restore.NewRestorer(cfg.Postgres, "", slog.Default())
	contents, err := restorer.ListArchiveContents(ctx, archivePath)
	if err != nil {
		return err
	}

	fmt.Printf("\nArchive contents for: %s\n\n", archivePath)
	fmt.Println(contents)
	return nil
}

// formatSize formats bytes to human readable format
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// truncateString truncates a string to max length
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}