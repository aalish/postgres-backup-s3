package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Session struct {
	ID               string     `bson:"_id" json:"id"`
	UserID           primitive.ObjectID `bson:"user_id,omitempty" json:"userId,omitempty"`
	Username         string     `bson:"username" json:"username"`
	RefreshTokenHash string     `bson:"refresh_token_hash" json:"-"`
	CSRFToken        string     `bson:"csrf_token" json:"-"`
	ExpiresAt        time.Time  `bson:"expires_at" json:"expiresAt"`
	CreatedAt        time.Time  `bson:"created_at" json:"createdAt"`
	UpdatedAt        time.Time  `bson:"updated_at" json:"updatedAt"`
	LastSeenAt       time.Time  `bson:"last_seen_at" json:"lastSeenAt"`
	RevokedAt        *time.Time `bson:"revoked_at,omitempty" json:"revokedAt,omitempty"`
	IPAddress        string     `bson:"ip_address,omitempty" json:"ipAddress,omitempty"`
	UserAgent        string     `bson:"user_agent,omitempty" json:"userAgent,omitempty"`
}

type BackupRun struct {
	ID              string     `bson:"_id" json:"id"`
	DatabaseID      string     `bson:"database_id,omitempty" json:"databaseId,omitempty"`
	DatabaseName    string     `bson:"database_name,omitempty" json:"databaseName,omitempty"`
	TriggeredBy     string     `bson:"triggered_by" json:"triggeredBy"`
	TriggerSource   string     `bson:"trigger_source" json:"triggerSource"`
	Status          string     `bson:"status" json:"status"`
	ArchiveFilename string     `bson:"archive_filename,omitempty" json:"archiveFilename,omitempty"`
	ArchivePath     string     `bson:"archive_path,omitempty" json:"archivePath,omitempty"`
	S3Key           string     `bson:"s3_key,omitempty" json:"s3Key,omitempty"`
	S3URI           string     `bson:"s3_uri,omitempty" json:"s3URI,omitempty"`
	SizeBytes       int64      `bson:"size_bytes,omitempty" json:"sizeBytes,omitempty"`
	Error           string     `bson:"error,omitempty" json:"error,omitempty"`
	StartedAt       time.Time  `bson:"started_at" json:"startedAt"`
	FinishedAt      *time.Time `bson:"finished_at,omitempty" json:"finishedAt,omitempty"`
}

type RetentionRun struct {
	ID           string     `bson:"_id" json:"id"`
	DatabaseID   string     `bson:"database_id,omitempty" json:"databaseId,omitempty"`
	DatabaseName string     `bson:"database_name,omitempty" json:"databaseName,omitempty"`
	Trigger      string     `bson:"trigger" json:"trigger"`
	Status       string     `bson:"status" json:"status"`
	Evaluated    int        `bson:"evaluated" json:"evaluated"`
	Deleted      int        `bson:"deleted" json:"deleted"`
	DeletedKeys  []string   `bson:"deleted_keys,omitempty" json:"deletedKeys,omitempty"`
	Error        string     `bson:"error,omitempty" json:"error,omitempty"`
	StartedAt    time.Time  `bson:"started_at" json:"startedAt"`
	FinishedAt   *time.Time `bson:"finished_at,omitempty" json:"finishedAt,omitempty"`
}

// RestoreRun represents a restore job execution
type RestoreRun struct {
	ID             string            `bson:"_id" json:"id"`
	DatabaseID     string            `bson:"database_id,omitempty" json:"databaseId,omitempty"`
	TargetDatabase string            `bson:"target_database" json:"targetDatabase"`
	SourceBackupID string            `bson:"source_backup_id,omitempty" json:"sourceBackupId,omitempty"`
	S3URI          string            `bson:"s3_uri,omitempty" json:"s3Uri,omitempty"`
	S3Key          string            `bson:"s3_key,omitempty" json:"s3Key,omitempty"`
	LocalPath      string            `bson:"local_path,omitempty" json:"localPath,omitempty"`
	TriggeredBy    string            `bson:"triggered_by" json:"triggeredBy"`
	TriggerSource  string            `bson:"trigger_source" json:"triggerSource"`
	Status         string            `bson:"status" json:"status"`
	Options        RestoreOptions    `bson:"options" json:"options"`
	Error          string            `bson:"error,omitempty" json:"error,omitempty"`
	Warnings       []string          `bson:"warnings,omitempty" json:"warnings,omitempty"`
	StartedAt      time.Time         `bson:"started_at" json:"startedAt"`
	FinishedAt     *time.Time        `bson:"finished_at,omitempty" json:"finishedAt,omitempty"`
	DurationMs     int64             `bson:"duration_ms,omitempty" json:"durationMs,omitempty"`
	DownloadTimeMs int64             `bson:"download_time_ms,omitempty" json:"downloadTimeMs,omitempty"`
	RestoreTimeMs  int64             `bson:"restore_time_ms,omitempty" json:"restoreTimeMs,omitempty"`
}

// RestoreOptions stores the options used for a restore operation
type RestoreOptions struct {
	Clean            bool     `bson:"clean" json:"clean"`
	CreateDB         bool     `bson:"create_db" json:"createDb"`
	IfExists         bool     `bson:"if_exists" json:"ifExists"`
	NoOwner          bool     `bson:"no_owner" json:"noOwner"`
	NoPrivileges     bool     `bson:"no_privileges" json:"noPrivileges"`
	DataOnly         bool     `bson:"data_only" json:"dataOnly"`
	SchemaOnly       bool     `bson:"schema_only" json:"schemaOnly"`
	SingleTransaction bool     `bson:"single_transaction" json:"singleTransaction"`
	Jobs             int      `bson:"jobs,omitempty" json:"jobs,omitempty"`
	Schemas          []string `bson:"schemas,omitempty" json:"schemas,omitempty"`
	Tables           []string `bson:"tables,omitempty" json:"tables,omitempty"`
	ExcludeSchemas   []string `bson:"exclude_schemas,omitempty" json:"excludeSchemas,omitempty"`
	ExcludeTables    []string `bson:"exclude_tables,omitempty" json:"excludeTables,omitempty"`
}

// DatabaseConfig represents a database configuration stored in MongoDB
type DatabaseConfig struct {
	ID               string    `bson:"_id,omitempty" json:"id,omitempty"`
	DatabaseID       string    `bson:"database_id" json:"databaseId"`
	Name             string    `bson:"name" json:"name"`
	Host             string    `bson:"host" json:"host"`
	Port             int       `bson:"port" json:"port"`
	Username         string    `bson:"username" json:"username"`
	PasswordEncrypted string   `bson:"password_encrypted,omitempty" json:"-"`
	Database         string    `bson:"database" json:"database"`
	SSLMode          string    `bson:"ssl_mode,omitempty" json:"sslMode,omitempty"`
	Enabled          bool      `bson:"enabled" json:"enabled"`
	BackupSchedule   string    `bson:"backup_schedule,omitempty" json:"backupSchedule,omitempty"`
	RetentionDays    int       `bson:"retention_days,omitempty" json:"retentionDays,omitempty"`
	CompressionLevel int       `bson:"compression_level,omitempty" json:"compressionLevel,omitempty"`
	S3Prefix         string    `bson:"s3_prefix,omitempty" json:"s3Prefix,omitempty"`
	FilenamePrefix   string    `bson:"filename_prefix,omitempty" json:"filenamePrefix,omitempty"`
	Tags             []string  `bson:"tags,omitempty" json:"tags,omitempty"`
	CreatedAt        time.Time `bson:"created_at" json:"createdAt"`
	UpdatedAt        time.Time `bson:"updated_at" json:"updatedAt"`
	CreatedBy        string    `bson:"created_by" json:"createdBy"`
	UpdatedBy        string    `bson:"updated_by" json:"updatedBy"`
	LastBackupAt     *time.Time `bson:"last_backup_at,omitempty" json:"lastBackupAt,omitempty"`
	LastRestoreAt    *time.Time `bson:"last_restore_at,omitempty" json:"lastRestoreAt,omitempty"`
	TotalBackups     int        `bson:"total_backups,omitempty" json:"totalBackups,omitempty"`
	TotalRestores    int        `bson:"total_restores,omitempty" json:"totalRestores,omitempty"`
	LastBackupSize   int64      `bson:"last_backup_size,omitempty" json:"lastBackupSize,omitempty"`
	LastBackupStatus string     `bson:"last_backup_status,omitempty" json:"lastBackupStatus,omitempty"`
}

type AuditEvent struct {
	ID        string         `bson:"_id" json:"id"`
	Type      string         `bson:"type" json:"type"`
	Actor     string         `bson:"actor" json:"actor"`
	ActorID   primitive.ObjectID `bson:"actor_id,omitempty" json:"actorId,omitempty"`
	Message   string         `bson:"message" json:"message"`
	Metadata  map[string]any `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt time.Time      `bson:"created_at" json:"createdAt"`
	IPAddress string         `bson:"ip_address,omitempty" json:"ipAddress,omitempty"`
	UserAgent string         `bson:"user_agent,omitempty" json:"userAgent,omitempty"`
	DatabaseID string        `bson:"database_id,omitempty" json:"databaseId,omitempty"`
}

func NewID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}

	return hex.EncodeToString(buf[:])
}
