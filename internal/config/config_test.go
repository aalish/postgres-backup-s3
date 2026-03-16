package config

import (
	"os"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("PGHOST", "db.internal")
	t.Setenv("PGPORT", "5432")
	t.Setenv("PGUSER", "backup")
	t.Setenv("PGPASSWORD", "secret")
	t.Setenv("PGDATABASE", "app")
	t.Setenv("PG_DUMP_PATH", "pg_dump")
	t.Setenv("PG_RESTORE_PATH", "pg_restore")
	t.Setenv("BACKUP_OUTPUT_DIR", "./backups")
	t.Setenv("BACKUP_FILENAME_PREFIX", "")
	t.Setenv("BACKUP_COMPRESSION", "6")
	t.Setenv("S3_BUCKET", "backup-bucket")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("S3_PREFIX", "")
	t.Setenv("S3_USE_PATH_STYLE", "false")
	t.Setenv("S3_ENDPOINT_URL", "")
	t.Setenv("UPLOAD_MAX_ATTEMPTS", "5")
	t.Setenv("UPLOAD_INITIAL_DELAY", "2s")
	t.Setenv("UPLOAD_MAX_DELAY", "30s")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "json")
}

func TestParseAssignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		key     string
		value   string
		wantErr bool
	}{
		{name: "comment", line: "# hello"},
		{name: "plain", line: "PGHOST=db.internal", key: "PGHOST", value: "db.internal"},
		{name: "quoted", line: `PGPASSWORD="s3cret value"`, key: "PGPASSWORD", value: "s3cret value"},
		{name: "single quoted", line: "S3_PREFIX='nightly/main'", key: "S3_PREFIX", value: "nightly/main"},
		{name: "export prefix", line: "export AWS_REGION=us-east-1", key: "AWS_REGION", value: "us-east-1"},
		{name: "invalid", line: "PGHOST", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			key, value, err := ParseAssignment(tt.line)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAssignment() error = %v, wantErr %v", err, tt.wantErr)
			}
			if key != tt.key {
				t.Fatalf("ParseAssignment() key = %q, want %q", key, tt.key)
			}
			if value != tt.value {
				t.Fatalf("ParseAssignment() value = %q, want %q", value, tt.value)
			}
		})
	}
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Backup.FilenamePrefix != "app" {
		t.Fatalf("Load() filename prefix = %q, want %q", cfg.Backup.FilenamePrefix, "app")
	}
}

func TestLoadRejectsInvalidIntegerValue(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PGPORT", "not-a-number")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid integer error")
	}
}

func TestLoadEnvFileDoesNotOverrideExistingEnv(t *testing.T) {
	t.Setenv("PGHOST", "env-priority")

	file, err := os.CreateTemp(t.TempDir(), "*.env")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}

	content := "PGHOST=file-value\nPGPORT=5432\n"
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := LoadEnvFile(file.Name()); err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}

	if got := os.Getenv("PGHOST"); got != "env-priority" {
		t.Fatalf("PGHOST = %q, want %q", got, "env-priority")
	}
	if got := os.Getenv("PGPORT"); got != "5432" {
		t.Fatalf("PGPORT = %q, want %q", got, "5432")
	}
}
