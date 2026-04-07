package runtimecfg

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/neelgai/postgres-backup/internal/config"
)

const DocumentID = "runtime-config"

type Settings struct {
	ID                   string               `bson:"_id" json:"id"`
	RetentionDays        int                  `bson:"retention_days" json:"retentionDays"`
	BackupSchedule       string               `bson:"backup_schedule" json:"backupSchedule"`
	S3Bucket             string               `bson:"s3_bucket" json:"s3Bucket"`
	S3Prefix             string               `bson:"s3_prefix" json:"s3Prefix"`
	BackupOutputDir      string               `bson:"backup_output_dir" json:"backupOutputDir"`
	BackupFilenamePrefix string               `bson:"backup_filename_prefix" json:"backupFilenamePrefix"`
	BackupCompression    int                  `bson:"backup_compression" json:"backupCompression"`
	Notification         NotificationSettings `bson:"notification" json:"notification"`
	UpdatedAt            time.Time            `bson:"updated_at" json:"updatedAt"`
	UpdatedBy            string               `bson:"updated_by" json:"updatedBy"`
}

type NotificationSettings struct {
	Enabled        bool   `bson:"enabled" json:"enabled"`
	WebhookURL     string `bson:"webhook_url" json:"webhookURL"`
	WebhookTimeout string `bson:"webhook_timeout" json:"webhookTimeout"`
}

func Defaults(cfg config.DashboardConfig) Settings {
	return Settings{
		ID:                   DocumentID,
		RetentionDays:        cfg.Defaults.RetentionDays,
		BackupSchedule:       cfg.Defaults.BackupSchedule,
		S3Bucket:             cfg.Base.S3.Bucket,
		S3Prefix:             cfg.Base.S3.Prefix,
		BackupOutputDir:      cfg.Base.Backup.OutputDir,
		BackupFilenamePrefix: cfg.Base.Backup.FilenamePrefix,
		BackupCompression:    cfg.Base.Backup.Compression,
		Notification: NotificationSettings{
			Enabled:        cfg.Defaults.NotificationEnable,
			WebhookURL:     cfg.Defaults.WebhookURL,
			WebhookTimeout: cfg.Defaults.WebhookTimeout.String(),
		},
	}
}

func (s Settings) Validate() error {
	if s.RetentionDays < 1 {
		return fmt.Errorf("retention period must be at least 1 day")
	}
	if _, err := cron.ParseStandard(strings.TrimSpace(s.BackupSchedule)); err != nil {
		return fmt.Errorf("backup schedule must be a valid 5-field cron expression: %w", err)
	}
	if strings.TrimSpace(s.S3Bucket) == "" {
		return fmt.Errorf("S3 bucket is required")
	}
	if strings.TrimSpace(s.BackupOutputDir) == "" {
		return fmt.Errorf("backup output directory is required")
	}
	if strings.TrimSpace(s.BackupFilenamePrefix) == "" {
		return fmt.Errorf("backup filename prefix is required")
	}
	if s.BackupCompression < 0 || s.BackupCompression > 9 {
		return fmt.Errorf("backup compression must be between 0 and 9")
	}
	timeout, err := s.NotificationTimeout()
	if err != nil {
		return err
	}
	if timeout <= 0 {
		return fmt.Errorf("notification timeout must be greater than 0")
	}
	if s.Notification.Enabled {
		if strings.TrimSpace(s.Notification.WebhookURL) == "" {
			return fmt.Errorf("notification webhook URL is required when notifications are enabled")
		}
		parsed, err := url.Parse(strings.TrimSpace(s.Notification.WebhookURL))
		if err != nil {
			return fmt.Errorf("notification webhook URL must be a valid URL: %w", err)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return fmt.Errorf("notification webhook URL must use http or https")
		}
	}

	return nil
}

func (s Settings) NotificationTimeout() (time.Duration, error) {
	timeout, err := time.ParseDuration(strings.TrimSpace(s.Notification.WebhookTimeout))
	if err != nil {
		return 0, fmt.Errorf("notification timeout must be a valid duration: %w", err)
	}

	return timeout, nil
}

func (s Settings) Apply(base config.Config) config.Config {
	merged := base
	merged.S3.Bucket = strings.TrimSpace(s.S3Bucket)
	merged.S3.Prefix = strings.TrimSpace(s.S3Prefix)
	merged.Backup.OutputDir = strings.TrimSpace(s.BackupOutputDir)
	merged.Backup.FilenamePrefix = strings.TrimSpace(s.BackupFilenamePrefix)
	merged.Backup.Compression = s.BackupCompression
	return merged
}
