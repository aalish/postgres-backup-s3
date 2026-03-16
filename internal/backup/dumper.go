package backup

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/neelgai/postgres-backup/internal/config"
)

type Dumper struct {
	pgConfig     config.PostgresConfig
	backupConfig config.BackupConfig
	logger       *slog.Logger
	now          func() time.Time
}

func NewDumper(pgConfig config.PostgresConfig, backupConfig config.BackupConfig, logger *slog.Logger) *Dumper {
	return &Dumper{
		pgConfig:     pgConfig,
		backupConfig: backupConfig,
		logger:       logger,
		now:          time.Now,
	}
}

func (d *Dumper) Create(ctx context.Context) (Archive, error) {
	if err := os.MkdirAll(d.backupConfig.OutputDir, 0o750); err != nil {
		return Archive{}, fmt.Errorf("create backup directory: %w", err)
	}

	filename := BuildFilename(d.backupConfig.FilenamePrefix, d.now())
	archivePath := filepath.Join(d.backupConfig.OutputDir, filename)

	args := []string{
		"--host", d.pgConfig.Host,
		"--port", strconv.Itoa(d.pgConfig.Port),
		"--username", d.pgConfig.Username,
		"--dbname", d.pgConfig.Database,
		"--format=custom",
		"--compress=" + strconv.Itoa(d.backupConfig.Compression),
		"--blobs",
		"--create",
		"--no-password",
		"--file", archivePath,
	}

	command := exec.CommandContext(ctx, d.pgConfig.PGDumpPath, args...)
	command.Env = d.commandEnv()

	var stderr bytes.Buffer
	command.Stderr = &stderr

	d.logger.Info("starting PostgreSQL backup", "database", d.pgConfig.Database, "output_file", archivePath)
	d.logger.Debug("prepared pg_dump command", "binary", d.pgConfig.PGDumpPath, "args", args, "password_configured", d.pgConfig.Password != "")

	if err := command.Run(); err != nil {
		return Archive{}, fmt.Errorf("pg_dump failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stderrOutput := strings.TrimSpace(stderr.String()); stderrOutput != "" {
		d.logger.Warn("pg_dump completed with stderr output", "output", stderrOutput)
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		return Archive{}, fmt.Errorf("stat backup file: %w", err)
	}
	if info.Size() <= 0 {
		return Archive{}, fmt.Errorf("backup file %s is empty", archivePath)
	}

	archive := Archive{
		Path:     archivePath,
		Filename: filename,
		Size:     info.Size(),
	}

	d.logger.Info("backup file created", "path", archive.Path, "size_bytes", archive.Size)
	return archive, nil
}

func (d *Dumper) Validate(ctx context.Context, archive Archive) error {
	command := exec.CommandContext(ctx, d.pgConfig.PGRestorePath, "--list", archive.Path)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	d.logger.Info("validating backup archive", "path", archive.Path)
	d.logger.Debug("prepared pg_restore validation command", "binary", d.pgConfig.PGRestorePath, "args", []string{"--list", archive.Path})

	if err := command.Run(); err != nil {
		return fmt.Errorf("pg_restore validation failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stderrOutput := strings.TrimSpace(stderr.String()); stderrOutput != "" {
		d.logger.Warn("pg_restore validation completed with stderr output", "output", stderrOutput)
	}
	if stdout.Len() == 0 {
		return fmt.Errorf("pg_restore validation produced empty output for %s", archive.Path)
	}

	d.logger.Info("backup archive validation succeeded", "path", archive.Path, "manifest_bytes", stdout.Len())
	return nil
}

func (d *Dumper) commandEnv() []string {
	env := os.Environ()
	if d.pgConfig.Password != "" {
		env = append(env, "PGPASSWORD="+d.pgConfig.Password)
	}
	return env
}
