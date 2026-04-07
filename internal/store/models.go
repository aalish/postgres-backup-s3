package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Session struct {
	ID               string     `bson:"_id" json:"id"`
	Username         string     `bson:"username" json:"username"`
	RefreshTokenHash string     `bson:"refresh_token_hash" json:"-"`
	CSRFToken        string     `bson:"csrf_token" json:"-"`
	ExpiresAt        time.Time  `bson:"expires_at" json:"expiresAt"`
	CreatedAt        time.Time  `bson:"created_at" json:"createdAt"`
	UpdatedAt        time.Time  `bson:"updated_at" json:"updatedAt"`
	LastSeenAt       time.Time  `bson:"last_seen_at" json:"lastSeenAt"`
	RevokedAt        *time.Time `bson:"revoked_at,omitempty" json:"revokedAt,omitempty"`
}

type BackupRun struct {
	ID              string     `bson:"_id" json:"id"`
	TriggeredBy     string     `bson:"triggered_by" json:"triggeredBy"`
	TriggerSource   string     `bson:"trigger_source" json:"triggerSource"`
	Status          string     `bson:"status" json:"status"`
	ArchiveFilename string     `bson:"archive_filename,omitempty" json:"archiveFilename,omitempty"`
	ArchivePath     string     `bson:"archive_path,omitempty" json:"archivePath,omitempty"`
	S3Key           string     `bson:"s3_key,omitempty" json:"s3Key,omitempty"`
	S3URI           string     `bson:"s3_uri,omitempty" json:"s3URI,omitempty"`
	Error           string     `bson:"error,omitempty" json:"error,omitempty"`
	StartedAt       time.Time  `bson:"started_at" json:"startedAt"`
	FinishedAt      *time.Time `bson:"finished_at,omitempty" json:"finishedAt,omitempty"`
}

type RetentionRun struct {
	ID          string     `bson:"_id" json:"id"`
	Trigger     string     `bson:"trigger" json:"trigger"`
	Status      string     `bson:"status" json:"status"`
	Evaluated   int        `bson:"evaluated" json:"evaluated"`
	Deleted     int        `bson:"deleted" json:"deleted"`
	DeletedKeys []string   `bson:"deleted_keys,omitempty" json:"deletedKeys,omitempty"`
	Error       string     `bson:"error,omitempty" json:"error,omitempty"`
	StartedAt   time.Time  `bson:"started_at" json:"startedAt"`
	FinishedAt  *time.Time `bson:"finished_at,omitempty" json:"finishedAt,omitempty"`
}

type AuditEvent struct {
	ID        string         `bson:"_id" json:"id"`
	Type      string         `bson:"type" json:"type"`
	Actor     string         `bson:"actor" json:"actor"`
	Message   string         `bson:"message" json:"message"`
	Metadata  map[string]any `bson:"metadata,omitempty" json:"metadata,omitempty"`
	CreatedAt time.Time      `bson:"created_at" json:"createdAt"`
}

func NewID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}

	return hex.EncodeToString(buf[:])
}
