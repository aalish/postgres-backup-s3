package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/runtimecfg"
)

var ErrNotFound = errors.New("document not found")

const (
	settingsCollection   = "settings"
	sessionsCollection   = "sessions"
	backupRunsCollection = "backup_runs"
	retentionCollection  = "retention_runs"
	auditCollection      = "audit_events"
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
	logger *slog.Logger
}

func New(ctx context.Context, cfg config.MongoConfig, logger *slog.Logger) (*Store, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}

	store := &Store{
		client: client,
		db:     client.Database(cfg.Database),
		logger: logger,
	}
	if err := store.ensureIndexes(ctx); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	_, err := s.db.Collection(sessionsCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().
				SetExpireAfterSeconds(0).
				SetName("session_expiry_ttl"),
		},
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetName("session_username_idx"),
		},
	})
	if err != nil {
		return fmt.Errorf("create MongoDB session indexes: %w", err)
	}

	return nil
}

func (s *Store) GetSettings(ctx context.Context) (runtimecfg.Settings, bool, error) {
	var settings runtimecfg.Settings
	err := s.db.Collection(settingsCollection).
		FindOne(ctx, bson.M{"_id": runtimecfg.DocumentID}).
		Decode(&settings)
	if err == nil {
		return settings, true, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return runtimecfg.Settings{}, false, nil
	}

	return runtimecfg.Settings{}, false, fmt.Errorf("find runtime settings: %w", err)
}

func (s *Store) SaveSettings(ctx context.Context, settings runtimecfg.Settings) error {
	if settings.ID == "" {
		settings.ID = runtimecfg.DocumentID
	}
	_, err := s.db.Collection(settingsCollection).ReplaceOne(
		ctx,
		bson.M{"_id": settings.ID},
		settings,
		options.Replace().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("save runtime settings: %w", err)
	}

	return nil
}

func (s *Store) CreateSession(ctx context.Context, session Session) error {
	if _, err := s.db.Collection(sessionsCollection).InsertOne(ctx, session); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var session Session
	err := s.db.Collection(sessionsCollection).FindOne(ctx, bson.M{"_id": id}).Decode(&session)
	if err == nil {
		return session, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Session{}, ErrNotFound
	}

	return Session{}, fmt.Errorf("find session: %w", err)
}

func (s *Store) SaveSession(ctx context.Context, session Session) error {
	_, err := s.db.Collection(sessionsCollection).ReplaceOne(ctx, bson.M{"_id": session.ID}, session)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *Store) RevokeSession(ctx context.Context, id string, revokedAt time.Time) error {
	update := bson.M{
		"$set": bson.M{
			"revoked_at": revokedAt.UTC(),
			"updated_at": revokedAt.UTC(),
		},
	}
	result, err := s.db.Collection(sessionsCollection).UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *Store) InsertBackupRun(ctx context.Context, run BackupRun) error {
	if _, err := s.db.Collection(backupRunsCollection).InsertOne(ctx, run); err != nil {
		return fmt.Errorf("insert backup run: %w", err)
	}
	return nil
}

func (s *Store) UpdateBackupRun(ctx context.Context, run BackupRun) error {
	_, err := s.db.Collection(backupRunsCollection).ReplaceOne(ctx, bson.M{"_id": run.ID}, run)
	if err != nil {
		return fmt.Errorf("update backup run: %w", err)
	}
	return nil
}

func (s *Store) ListBackupRuns(ctx context.Context, page, pageSize int) ([]BackupRun, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	collection := s.db.Collection(backupRunsCollection)
	total, err := collection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return nil, 0, fmt.Errorf("count backup runs: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "started_at", Value: -1}}).
		SetSkip(int64((page - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := collection.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list backup runs: %w", err)
	}
	defer cursor.Close(ctx)

	var runs []BackupRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, fmt.Errorf("decode backup runs: %w", err)
	}

	return runs, total, nil
}

func (s *Store) LatestBackupRun(ctx context.Context) (*BackupRun, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "started_at", Value: -1}})
	var run BackupRun
	err := s.db.Collection(backupRunsCollection).FindOne(ctx, bson.D{}, opts).Decode(&run)
	if err == nil {
		return &run, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}

	return nil, fmt.Errorf("find latest backup run: %w", err)
}

func (s *Store) InsertRetentionRun(ctx context.Context, run RetentionRun) error {
	if _, err := s.db.Collection(retentionCollection).InsertOne(ctx, run); err != nil {
		return fmt.Errorf("insert retention run: %w", err)
	}
	return nil
}

func (s *Store) UpdateRetentionRun(ctx context.Context, run RetentionRun) error {
	_, err := s.db.Collection(retentionCollection).ReplaceOne(ctx, bson.M{"_id": run.ID}, run)
	if err != nil {
		return fmt.Errorf("update retention run: %w", err)
	}
	return nil
}

func (s *Store) ListRetentionRuns(ctx context.Context, page, pageSize int) ([]RetentionRun, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	collection := s.db.Collection(retentionCollection)
	total, err := collection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return nil, 0, fmt.Errorf("count retention runs: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "started_at", Value: -1}}).
		SetSkip(int64((page - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := collection.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list retention runs: %w", err)
	}
	defer cursor.Close(ctx)

	var runs []RetentionRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, fmt.Errorf("decode retention runs: %w", err)
	}

	return runs, total, nil
}

func (s *Store) InsertAuditEvent(ctx context.Context, event AuditEvent) error {
	if _, err := s.db.Collection(auditCollection).InsertOne(ctx, event); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s *Store) ListAuditEvents(ctx context.Context, page, pageSize int) ([]AuditEvent, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	collection := s.db.Collection(auditCollection)
	total, err := collection.CountDocuments(ctx, bson.D{})
	if err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetSkip(int64((page - 1) * pageSize)).
		SetLimit(int64(pageSize))
	cursor, err := collection.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
	}
	defer cursor.Close(ctx)

	var events []AuditEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, 0, fmt.Errorf("decode audit events: %w", err)
	}

	return events, total, nil
}
