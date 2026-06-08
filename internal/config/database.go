package config

import (
	"fmt"
	"strings"
)

// DatabaseConfig represents configuration for a single PostgreSQL database
type DatabaseConfig struct {
	// Unique identifier for this database configuration
	ID string `json:"id"`

	// Display name for the database in the UI
	Name string `json:"name"`

	// PostgreSQL connection settings
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"-"` // Don't serialize password
	Database string `json:"database"`

	// Optional: SSL mode (disable, require, verify-ca, verify-full)
	SSLMode string `json:"ssl_mode,omitempty"`

	// Backup-specific settings for this database
	Enabled            bool   `json:"enabled"`
	BackupSchedule     string `json:"backup_schedule,omitempty"`      // Override global schedule
	RetentionDays      int    `json:"retention_days,omitempty"`       // Override global retention
	CompressionLevel   int    `json:"compression_level,omitempty"`    // 0-9
	S3Prefix           string `json:"s3_prefix,omitempty"`            // Database-specific S3 prefix

	// Backup filename prefix (defaults to database name if empty)
	FilenamePrefix string `json:"filename_prefix,omitempty"`

	// Tags for organization and filtering
	Tags []string `json:"tags,omitempty"`
}

// Validate checks if the database configuration is valid
func (dc *DatabaseConfig) Validate() error {
	if dc.ID == "" {
		return fmt.Errorf("database ID is required")
	}
	if dc.Name == "" {
		return fmt.Errorf("database name is required")
	}
	if dc.Host == "" {
		return fmt.Errorf("database host is required for %s", dc.Name)
	}
	if dc.Port <= 0 || dc.Port > 65535 {
		return fmt.Errorf("invalid port %d for database %s", dc.Port, dc.Name)
	}
	if dc.User == "" {
		return fmt.Errorf("database user is required for %s", dc.Name)
	}
	if dc.Database == "" {
		return fmt.Errorf("database name is required for %s", dc.Name)
	}

	// Validate SSL mode if specified
	if dc.SSLMode != "" {
		validSSLModes := []string{"disable", "require", "verify-ca", "verify-full"}
		valid := false
		for _, mode := range validSSLModes {
			if dc.SSLMode == mode {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid SSL mode %s for database %s", dc.SSLMode, dc.Name)
		}
	}

	// Validate compression level
	if dc.CompressionLevel < 0 || dc.CompressionLevel > 9 {
		return fmt.Errorf("compression level must be between 0-9 for database %s", dc.Name)
	}

	// Validate backup schedule if specified
	if dc.BackupSchedule != "" {
		// Basic cron validation (could be enhanced)
		fields := strings.Fields(dc.BackupSchedule)
		if len(fields) != 5 && len(fields) != 6 {
			return fmt.Errorf("invalid cron schedule for database %s: %s", dc.Name, dc.BackupSchedule)
		}
	}

	return nil
}

// ConnectionString returns the PostgreSQL connection string for this database
func (dc *DatabaseConfig) ConnectionString() string {
	parts := []string{
		fmt.Sprintf("host=%s", dc.Host),
		fmt.Sprintf("port=%d", dc.Port),
		fmt.Sprintf("user=%s", dc.User),
		fmt.Sprintf("dbname=%s", dc.Database),
	}

	if dc.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", dc.Password))
	}

	if dc.SSLMode != "" {
		parts = append(parts, fmt.Sprintf("sslmode=%s", dc.SSLMode))
	} else {
		parts = append(parts, "sslmode=disable")
	}

	return strings.Join(parts, " ")
}

// GetFilenamePrefix returns the filename prefix to use for backups
func (dc *DatabaseConfig) GetFilenamePrefix() string {
	if dc.FilenamePrefix != "" {
		return dc.FilenamePrefix
	}
	// Default to sanitized database name
	return sanitizeFilename(dc.Database)
}

// GetS3Prefix returns the S3 prefix for this database
func (dc *DatabaseConfig) GetS3Prefix(globalPrefix string) string {
	if dc.S3Prefix != "" {
		return dc.S3Prefix
	}
	// Use global prefix with database ID subdirectory
	if globalPrefix != "" {
		return fmt.Sprintf("%s/%s", strings.TrimSuffix(globalPrefix, "/"), dc.ID)
	}
	return dc.ID
}

// sanitizeFilename removes or replaces characters that might cause issues in filenames
func sanitizeFilename(name string) string {
	// Replace spaces and special characters with underscores
	replacer := strings.NewReplacer(
		" ", "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		".", "_",
	)
	return replacer.Replace(name)
}

// DatabaseList represents a collection of database configurations
type DatabaseList struct {
	Databases []DatabaseConfig `json:"databases"`
}

// Validate checks if all database configurations are valid and IDs are unique
func (dl *DatabaseList) Validate() error {
	ids := make(map[string]bool)
	for i, db := range dl.Databases {
		if err := db.Validate(); err != nil {
			return fmt.Errorf("database[%d]: %w", i, err)
		}
		if ids[db.ID] {
			return fmt.Errorf("duplicate database ID: %s", db.ID)
		}
		ids[db.ID] = true
	}
	return nil
}

// GetByID returns a database configuration by its ID
func (dl *DatabaseList) GetByID(id string) (*DatabaseConfig, error) {
	for i := range dl.Databases {
		if dl.Databases[i].ID == id {
			return &dl.Databases[i], nil
		}
	}
	return nil, fmt.Errorf("database with ID %s not found", id)
}

// GetEnabled returns all enabled database configurations
func (dl *DatabaseList) GetEnabled() []DatabaseConfig {
	var enabled []DatabaseConfig
	for _, db := range dl.Databases {
		if db.Enabled {
			enabled = append(enabled, db)
		}
	}
	return enabled
}