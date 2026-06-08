package restore

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/neelgai/postgres-backup/internal/backup"
	"github.com/neelgai/postgres-backup/internal/config"
)

// Restorer handles PostgreSQL database restoration from backup archives
type Restorer struct {
	postgresConfig config.PostgresConfig
	outputDir      string
	logger         *slog.Logger
}

// RestoreOptions contains options for database restoration
type RestoreOptions struct {
	// Core options
	ArchivePath string // Path to the backup archive file
	TargetDB    string // Target database name (defaults to original)

	// Restore behavior options
	Clean           bool // Drop database objects before recreating them
	CreateDB        bool // Create the database before restoring
	IfExists        bool // Use IF EXISTS when dropping objects
	NoOwner         bool // Do not restore ownership
	NoPrivileges    bool // Do not restore privileges
	NoTablespaces   bool // Do not restore tablespace assignments
	DataOnly        bool // Restore only data, not schema
	SchemaOnly      bool // Restore only schema, not data
	SingleTransction bool // Restore as a single transaction

	// Selective restore options
	Schemas        []string // Specific schemas to restore
	Tables         []string // Specific tables to restore
	ExcludeSchemas []string // Schemas to exclude from restore
	ExcludeTables  []string // Tables to exclude from restore

	// Performance options
	Jobs int // Number of parallel jobs for restore

	// Safety options
	DryRun      bool // Show what would be restored without actually doing it
	Verbose     bool // Verbose output
	ForceRestore bool // Force restore even if warnings exist
}

// RestoreResult contains the result of a restore operation
type RestoreResult struct {
	DatabaseName string
	ArchivePath  string
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	Success      bool
	Message      string
	Warnings     []string
}

// NewRestorer creates a new database restorer
func NewRestorer(postgresConfig config.PostgresConfig, outputDir string, logger *slog.Logger) *Restorer {
	return &Restorer{
		postgresConfig: postgresConfig,
		outputDir:      outputDir,
		logger:         logger,
	}
}

// ValidateArchive validates that a backup archive can be restored
func (r *Restorer) ValidateArchive(ctx context.Context, archivePath string) (*backup.Archive, error) {
	// Check if file exists
	fileInfo, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("archive file not found: %w", err)
	}

	// Check file is not empty
	if fileInfo.Size() == 0 {
		return nil, fmt.Errorf("archive file is empty")
	}

	archive := &backup.Archive{
		Filename: filepath.Base(archivePath),
		Path:     archivePath,
		Size:     fileInfo.Size(),
	}

	// Use pg_restore -l to list contents and validate format
	args := []string{
		"-l", // List archive contents
		archivePath,
	}

	cmd := exec.CommandContext(ctx, r.postgresConfig.PGRestorePath, args...)
	cmd.Env = r.buildEnvironment()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("archive validation failed: %w (output: %s)", err, string(output))
	}

	// Check if output contains expected PostgreSQL archive header
	outputStr := string(output)
	if !strings.Contains(outputStr, "Archive created") && !strings.Contains(outputStr, "TOC") {
		return nil, fmt.Errorf("file does not appear to be a valid PostgreSQL archive")
	}

	r.logger.Info("archive validated successfully", "path", archivePath, "size", archive.Size)
	return archive, nil
}

// ListArchiveContents lists the contents of a backup archive
func (r *Restorer) ListArchiveContents(ctx context.Context, archivePath string) (string, error) {
	args := []string{
		"-l", // List archive contents
		archivePath,
	}

	cmd := exec.CommandContext(ctx, r.postgresConfig.PGRestorePath, args...)
	cmd.Env = r.buildEnvironment()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list archive contents: %w", err)
	}

	return string(output), nil
}

// Restore performs the database restoration
func (r *Restorer) Restore(ctx context.Context, options RestoreOptions) (*RestoreResult, error) {
	startTime := time.Now()
	result := &RestoreResult{
		DatabaseName: options.TargetDB,
		ArchivePath:  options.ArchivePath,
		StartTime:    startTime,
		Warnings:     []string{},
	}

	// Validate archive first
	archive, err := r.ValidateArchive(ctx, options.ArchivePath)
	if err != nil {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		result.Success = false
		result.Message = fmt.Sprintf("Archive validation failed: %v", err)
		return result, err
	}

	// If target database not specified, use the original database name
	if options.TargetDB == "" {
		options.TargetDB = r.postgresConfig.Database
		result.DatabaseName = options.TargetDB
	}

	// Build pg_restore command arguments
	args := r.buildRestoreArgs(options)

	r.logger.Info("starting database restore",
		"database", options.TargetDB,
		"archive", archive.Filename,
		"size", archive.Size,
		"options", fmt.Sprintf("%+v", options))

	if options.DryRun {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(startTime)
		result.Success = true
		result.Message = fmt.Sprintf("DRY RUN: Would restore from %s to database %s", archive.Filename, options.TargetDB)
		r.logger.Info("dry run completed", "command", fmt.Sprintf("%s %s", r.postgresConfig.PGRestorePath, strings.Join(args, " ")))
		return result, nil
	}

	// Execute pg_restore
	cmd := exec.CommandContext(ctx, r.postgresConfig.PGRestorePath, args...)
	cmd.Env = r.buildRestoreEnvironment(options.TargetDB)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Check for warnings in output
	if strings.Contains(outputStr, "WARNING:") {
		warnings := r.extractWarnings(outputStr)
		result.Warnings = warnings
		r.logger.Warn("restore completed with warnings", "warnings", warnings)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)

	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Restore failed: %v (output: %s)", err, outputStr)
		r.logger.Error("restore failed", "error", err, "output", outputStr, "duration", result.Duration)

		// If not forcing, fail on errors
		if !options.ForceRestore {
			return result, fmt.Errorf("restore failed: %w", err)
		}

		// If forcing, log but continue
		r.logger.Warn("forcing restore despite errors", "error", err)
		result.Success = true
		result.Message = fmt.Sprintf("Restore completed with errors (forced): %v", err)
	} else {
		result.Success = true
		result.Message = fmt.Sprintf("Successfully restored database %s from %s", options.TargetDB, archive.Filename)
		r.logger.Info("restore completed successfully",
			"database", options.TargetDB,
			"archive", archive.Filename,
			"duration", result.Duration)
	}

	return result, nil
}

// buildRestoreArgs builds the pg_restore command arguments
func (r *Restorer) buildRestoreArgs(options RestoreOptions) []string {
	args := []string{
		"-h", r.postgresConfig.Host,
		"-p", fmt.Sprintf("%d", r.postgresConfig.Port),
		"-U", r.postgresConfig.Username,
		"-d", options.TargetDB,
	}

	// Add behavior options
	if options.Clean {
		args = append(args, "--clean")
	}
	if options.CreateDB {
		args = append(args, "--create")
	}
	if options.IfExists {
		args = append(args, "--if-exists")
	}
	if options.NoOwner {
		args = append(args, "--no-owner")
	}
	if options.NoPrivileges {
		args = append(args, "--no-privileges")
	}
	if options.NoTablespaces {
		args = append(args, "--no-tablespaces")
	}
	if options.DataOnly {
		args = append(args, "--data-only")
	}
	if options.SchemaOnly {
		args = append(args, "--schema-only")
	}
	if options.SingleTransction {
		args = append(args, "--single-transaction")
	}
	if options.Verbose {
		args = append(args, "--verbose")
	}

	// Add selective restore options
	for _, schema := range options.Schemas {
		args = append(args, "--schema", schema)
	}
	for _, table := range options.Tables {
		args = append(args, "--table", table)
	}
	for _, schema := range options.ExcludeSchemas {
		args = append(args, "--exclude-schema", schema)
	}
	for _, table := range options.ExcludeTables {
		args = append(args, "--exclude-table", table)
	}

	// Add performance options
	if options.Jobs > 1 {
		args = append(args, "-j", fmt.Sprintf("%d", options.Jobs))
	}

	// Add the archive path
	args = append(args, options.ArchivePath)

	return args
}

// inheritedEnvWithout returns os.Environ() with the given variable names
// removed. We strip vars before re-setting them so that POSIX getenv (which
// returns the FIRST match) sees the value we intend, not an inherited shadow.
func inheritedEnvWithout(strip ...string) []string {
	parent := os.Environ()
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		drop := false
		for _, name := range strip {
			if strings.HasPrefix(kv, name+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// buildEnvironment builds the environment variables for pg_restore
func (r *Restorer) buildEnvironment() []string {
	env := inheritedEnvWithout("PGPASSWORD")
	if r.postgresConfig.Password != "" {
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", r.postgresConfig.Password))
	}
	return env
}

// buildRestoreEnvironment builds environment variables for restore with target database
func (r *Restorer) buildRestoreEnvironment(targetDB string) []string {
	strip := []string{"PGPASSWORD"}
	if targetDB != "" && targetDB != r.postgresConfig.Database {
		strip = append(strip, "PGDATABASE")
	}
	env := inheritedEnvWithout(strip...)
	if r.postgresConfig.Password != "" {
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", r.postgresConfig.Password))
	}
	if targetDB != "" && targetDB != r.postgresConfig.Database {
		env = append(env, fmt.Sprintf("PGDATABASE=%s", targetDB))
	}
	return env
}

// extractWarnings extracts warning messages from pg_restore output
func (r *Restorer) extractWarnings(output string) []string {
	var warnings []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "WARNING:") {
			warnings = append(warnings, strings.TrimSpace(line))
		}
	}
	return warnings
}

// CreateDatabaseIfNotExists creates the target database if it doesn't exist
func (r *Restorer) CreateDatabaseIfNotExists(ctx context.Context, dbName string) error {
	// Check if database exists
	checkCmd := exec.CommandContext(ctx, "psql",
		"-h", r.postgresConfig.Host,
		"-p", fmt.Sprintf("%d", r.postgresConfig.Port),
		"-U", r.postgresConfig.Username,
		"-d", "postgres", // Connect to postgres database
		"-t", "-c", fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s'", dbName))

	checkCmd.Env = r.buildEnvironment()
	output, err := checkCmd.Output()

	// If database exists, return
	if err == nil && strings.TrimSpace(string(output)) == "1" {
		r.logger.Info("database already exists", "database", dbName)
		return nil
	}

	// Create database
	createCmd := exec.CommandContext(ctx, "psql",
		"-h", r.postgresConfig.Host,
		"-p", fmt.Sprintf("%d", r.postgresConfig.Port),
		"-U", r.postgresConfig.Username,
		"-d", "postgres", // Connect to postgres database
		"-c", fmt.Sprintf("CREATE DATABASE \"%s\"", dbName))

	createCmd.Env = r.buildEnvironment()
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create database %s: %w (output: %s)", dbName, err, string(output))
	}

	r.logger.Info("database created successfully", "database", dbName)
	return nil
}

// DropDatabaseIfExists drops the target database if it exists
func (r *Restorer) DropDatabaseIfExists(ctx context.Context, dbName string) error {
	// Terminate existing connections
	terminateCmd := exec.CommandContext(ctx, "psql",
		"-h", r.postgresConfig.Host,
		"-p", fmt.Sprintf("%d", r.postgresConfig.Port),
		"-U", r.postgresConfig.Username,
		"-d", "postgres",
		"-c", fmt.Sprintf("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()", dbName))

	terminateCmd.Env = r.buildEnvironment()
	terminateCmd.Run() // Ignore errors as database might not exist

	// Drop database
	dropCmd := exec.CommandContext(ctx, "psql",
		"-h", r.postgresConfig.Host,
		"-p", fmt.Sprintf("%d", r.postgresConfig.Port),
		"-U", r.postgresConfig.Username,
		"-d", "postgres",
		"-c", fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\"", dbName))

	dropCmd.Env = r.buildEnvironment()
	if output, err := dropCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to drop database %s: %w (output: %s)", dbName, err, string(output))
	}

	r.logger.Info("database dropped successfully", "database", dbName)
	return nil
}