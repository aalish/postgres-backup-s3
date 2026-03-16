package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Log      LogConfig
	Postgres PostgresConfig
	Backup   BackupConfig
	S3       S3Config
	Retry    RetryConfig
}

type LogConfig struct {
	Level  string
	Format string
}

type PostgresConfig struct {
	Host          string
	Port          int
	Username      string
	Password      string
	Database      string
	PGDumpPath    string
	PGRestorePath string
}

type BackupConfig struct {
	OutputDir      string
	FilenamePrefix string
	Compression    int
}

type S3Config struct {
	Bucket          string
	Region          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	UsePathStyle    bool
	EndpointURL     string
}

type RetryConfig struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func (c Config) LogFields() []any {
	return []any{
		"postgres_host", c.Postgres.Host,
		"postgres_port", c.Postgres.Port,
		"postgres_database", c.Postgres.Database,
		"postgres_user", c.Postgres.Username,
		"postgres_password_configured", c.Postgres.Password != "",
		"pg_dump_path", c.Postgres.PGDumpPath,
		"pg_restore_path", c.Postgres.PGRestorePath,
		"backup_output_dir", c.Backup.OutputDir,
		"backup_filename_prefix", c.Backup.FilenamePrefix,
		"backup_compression", c.Backup.Compression,
		"s3_bucket", c.S3.Bucket,
		"s3_prefix", c.S3.Prefix,
		"s3_region", c.S3.Region,
		"s3_auth_mode", c.S3.CredentialMode(),
		"s3_use_path_style", c.S3.UsePathStyle,
		"s3_custom_endpoint_configured", c.S3.EndpointURL != "",
		"upload_max_attempts", c.Retry.MaxAttempts,
		"upload_initial_delay", c.Retry.InitialDelay.String(),
		"upload_max_delay", c.Retry.MaxDelay.String(),
		"log_level", c.Log.Level,
		"log_format", c.Log.Format,
	}
}

func Load() (Config, error) {
	port, err := readInt("PGPORT", 5432)
	if err != nil {
		return Config{}, err
	}
	compression, err := readInt("BACKUP_COMPRESSION", 6)
	if err != nil {
		return Config{}, err
	}
	usePathStyle, err := readBool("S3_USE_PATH_STYLE", false)
	if err != nil {
		return Config{}, err
	}
	maxAttempts, err := readInt("UPLOAD_MAX_ATTEMPTS", 5)
	if err != nil {
		return Config{}, err
	}
	initialDelay, err := readDuration("UPLOAD_INITIAL_DELAY", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	maxDelay, err := readDuration("UPLOAD_MAX_DELAY", 30*time.Second)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		Postgres: PostgresConfig{
			Host:          strings.TrimSpace(os.Getenv("PGHOST")),
			Port:          port,
			Username:      strings.TrimSpace(os.Getenv("PGUSER")),
			Password:      os.Getenv("PGPASSWORD"),
			Database:      strings.TrimSpace(os.Getenv("PGDATABASE")),
			PGDumpPath:    getEnv("PG_DUMP_PATH", "pg_dump"),
			PGRestorePath: getEnv("PG_RESTORE_PATH", "pg_restore"),
		},
		Backup: BackupConfig{
			OutputDir:      getEnv("BACKUP_OUTPUT_DIR", filepath.Clean("./backups")),
			FilenamePrefix: strings.TrimSpace(os.Getenv("BACKUP_FILENAME_PREFIX")),
			Compression:    compression,
		},
		S3: S3Config{
			Bucket:          strings.TrimSpace(os.Getenv("S3_BUCKET")),
			Region:          strings.TrimSpace(os.Getenv("AWS_REGION")),
			Prefix:          strings.TrimSpace(os.Getenv("S3_PREFIX")),
			AccessKeyID:     strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
			SecretAccessKey: strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
			SessionToken:    strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN")),
			UsePathStyle:    usePathStyle,
			EndpointURL:     strings.TrimSpace(os.Getenv("S3_ENDPOINT_URL")),
		},
		Retry: RetryConfig{
			MaxAttempts:  maxAttempts,
			InitialDelay: initialDelay,
			MaxDelay:     maxDelay,
		},
	}

	if cfg.Backup.FilenamePrefix == "" {
		cfg.Backup.FilenamePrefix = cfg.Postgres.Database
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	var problems []string

	if c.Postgres.Host == "" {
		problems = append(problems, "PGHOST is required")
	}
	if c.Postgres.Port < 1 || c.Postgres.Port > 65535 {
		problems = append(problems, "PGPORT must be between 1 and 65535")
	}
	if c.Postgres.Username == "" {
		problems = append(problems, "PGUSER is required")
	}
	if c.Postgres.Database == "" {
		problems = append(problems, "PGDATABASE is required")
	}
	if c.Postgres.PGDumpPath == "" {
		problems = append(problems, "PG_DUMP_PATH is required")
	}
	if c.Postgres.PGRestorePath == "" {
		problems = append(problems, "PG_RESTORE_PATH is required")
	}
	if c.Backup.OutputDir == "" {
		problems = append(problems, "BACKUP_OUTPUT_DIR is required")
	}
	if c.Backup.Compression < 0 || c.Backup.Compression > 9 {
		problems = append(problems, "BACKUP_COMPRESSION must be between 0 and 9")
	}
	if c.S3.Bucket == "" {
		problems = append(problems, "S3_BUCKET is required")
	}
	if c.S3.Region == "" {
		problems = append(problems, "AWS_REGION is required")
	}
	if (c.S3.AccessKeyID == "") != (c.S3.SecretAccessKey == "") {
		problems = append(problems, "AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must either both be set or both be empty")
	}
	if c.Retry.MaxAttempts < 1 {
		problems = append(problems, "UPLOAD_MAX_ATTEMPTS must be at least 1")
	}
	if c.Retry.InitialDelay <= 0 {
		problems = append(problems, "UPLOAD_INITIAL_DELAY must be greater than 0")
	}
	if c.Retry.MaxDelay <= 0 {
		problems = append(problems, "UPLOAD_MAX_DELAY must be greater than 0")
	}
	if c.Retry.MaxDelay < c.Retry.InitialDelay {
		problems = append(problems, "UPLOAD_MAX_DELAY must be greater than or equal to UPLOAD_INITIAL_DELAY")
	}
	if !validLogFormat(c.Log.Format) {
		problems = append(problems, "LOG_FORMAT must be either text or json")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}

	return nil
}

func (c S3Config) UsesStaticCredentials() bool {
	return c.AccessKeyID != "" && c.SecretAccessKey != ""
}

func (c S3Config) CredentialMode() string {
	if c.UsesStaticCredentials() {
		return "static_credentials"
	}

	return "default_credential_chain"
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func readInt(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid integer: %w", key, err)
	}

	return parsed, nil
}

func readBool(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a valid boolean: %w", key, err)
	}

	return parsed, nil
}

func readDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}

	return parsed, nil
}

func validLogFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json", "text":
		return true
	default:
		return false
	}
}

func ParseAssignment(line string) (string, string, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", nil
	}

	if strings.HasPrefix(trimmed, "export ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}

	parts := strings.SplitN(trimmed, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid assignment: %q", line)
	}

	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", fmt.Errorf("missing key in assignment: %q", line)
	}

	value := strings.TrimSpace(parts[1])
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", "", fmt.Errorf("invalid quoted value for %s: %w", key, err)
		}
		value = unquoted
	} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}

	return key, value, nil
}
