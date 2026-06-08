package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Multi-database support methods

// BackupRunFilter represents filters for backup run queries
type BackupRunFilter struct {
	DatabaseID string
	Status     string
	StartDate  *time.Time
	EndDate    *time.Time
}

// RestoreRunFilter represents filters for restore run queries
type RestoreRunFilter struct {
	DatabaseID string
	Status     string
	StartDate  *time.Time
	EndDate    *time.Time
}

// GetLatestBackupRunForDatabase retrieves the most recent backup run for a specific database
func (s *Store) GetLatestBackupRunForDatabase(ctx context.Context, databaseID string) (*BackupRun, error) {
	filter := bson.M{"database_id": databaseID}
	opts := options.FindOne().SetSort(bson.D{{"started_at", -1}})

	var run BackupRun
	err := s.db.Collection("backup_runs").FindOne(ctx, filter, opts).Decode(&run)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find latest backup run: %w", err)
	}

	return &run, nil
}

// ListBackupRunsWithFilter retrieves backup runs with filtering
func (s *Store) ListBackupRunsWithFilter(ctx context.Context, filter BackupRunFilter, limit, offset int) ([]BackupRun, int64, error) {
	collection := s.db.Collection("backup_runs")

	// Build MongoDB filter
	mongoFilter := bson.M{}
	if filter.DatabaseID != "" {
		mongoFilter["database_id"] = filter.DatabaseID
	}
	if filter.Status != "" {
		mongoFilter["status"] = filter.Status
	}
	if filter.StartDate != nil {
		mongoFilter["started_at"] = bson.M{"$gte": filter.StartDate}
	}
	if filter.EndDate != nil {
		if mongoFilter["started_at"] == nil {
			mongoFilter["started_at"] = bson.M{}
		}
		mongoFilter["started_at"].(bson.M)["$lte"] = filter.EndDate
	}

	// Count total matching documents
	total, err := collection.CountDocuments(ctx, mongoFilter)
	if err != nil {
		return nil, 0, fmt.Errorf("count backup runs: %w", err)
	}

	// Query with pagination
	opts := options.Find().
		SetSort(bson.D{{"started_at", -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := collection.Find(ctx, mongoFilter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find backup runs: %w", err)
	}
	defer cursor.Close(ctx)

	var runs []BackupRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, fmt.Errorf("decode backup runs: %w", err)
	}

	return runs, total, nil
}

// Restore Run Methods

// InsertRestoreRun creates a new restore run record
func (s *Store) InsertRestoreRun(ctx context.Context, run RestoreRun) error {
	_, err := s.db.Collection("restore_runs").InsertOne(ctx, run)
	if err != nil {
		return fmt.Errorf("insert restore run: %w", err)
	}
	return nil
}

// UpdateRestoreRun updates an existing restore run
func (s *Store) UpdateRestoreRun(ctx context.Context, id string, updates bson.M) error {
	filter := bson.M{"_id": id}
	update := bson.M{"$set": updates}

	result, err := s.db.Collection("restore_runs").UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("update restore run: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("restore run not found: %s", id)
	}

	return nil
}

// GetRestoreRun retrieves a single restore run by ID
func (s *Store) GetRestoreRun(ctx context.Context, id string) (*RestoreRun, error) {
	filter := bson.M{"_id": id}

	var run RestoreRun
	err := s.db.Collection("restore_runs").FindOne(ctx, filter).Decode(&run)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("restore run not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("find restore run: %w", err)
	}

	return &run, nil
}

// ListRestoreRuns retrieves restore runs with pagination
func (s *Store) ListRestoreRuns(ctx context.Context, limit, offset int) ([]RestoreRun, error) {
	collection := s.db.Collection("restore_runs")

	opts := options.Find().
		SetSort(bson.D{{"started_at", -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("find restore runs: %w", err)
	}
	defer cursor.Close(ctx)

	var runs []RestoreRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, fmt.Errorf("decode restore runs: %w", err)
	}

	return runs, nil
}

// ListRestoreRunsWithFilter retrieves restore runs with filtering
func (s *Store) ListRestoreRunsWithFilter(ctx context.Context, filter RestoreRunFilter, limit, offset int) ([]RestoreRun, int64, error) {
	collection := s.db.Collection("restore_runs")

	// Build MongoDB filter
	mongoFilter := bson.M{}
	if filter.DatabaseID != "" {
		mongoFilter["database_id"] = filter.DatabaseID
	}
	if filter.Status != "" {
		mongoFilter["status"] = filter.Status
	}
	if filter.StartDate != nil {
		mongoFilter["started_at"] = bson.M{"$gte": filter.StartDate}
	}
	if filter.EndDate != nil {
		if mongoFilter["started_at"] == nil {
			mongoFilter["started_at"] = bson.M{}
		}
		mongoFilter["started_at"].(bson.M)["$lte"] = filter.EndDate
	}

	// Count total matching documents
	total, err := collection.CountDocuments(ctx, mongoFilter)
	if err != nil {
		return nil, 0, fmt.Errorf("count restore runs: %w", err)
	}

	// Query with pagination
	opts := options.Find().
		SetSort(bson.D{{"started_at", -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := collection.Find(ctx, mongoFilter, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("find restore runs: %w", err)
	}
	defer cursor.Close(ctx)

	var runs []RestoreRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, 0, fmt.Errorf("decode restore runs: %w", err)
	}

	return runs, total, nil
}

// GetLatestRestoreRunForDatabase retrieves the most recent restore run for a specific database
func (s *Store) GetLatestRestoreRunForDatabase(ctx context.Context, databaseID string) (*RestoreRun, error) {
	filter := bson.M{"database_id": databaseID}
	opts := options.FindOne().SetSort(bson.D{{"started_at", -1}})

	var run RestoreRun
	err := s.db.Collection("restore_runs").FindOne(ctx, filter, opts).Decode(&run)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find latest restore run: %w", err)
	}

	return &run, nil
}

// Database Configuration Methods

// ListDatabases retrieves all database configurations
func (s *Store) ListDatabases(ctx context.Context) ([]DatabaseConfig, error) {
	collection := s.db.Collection("databases")

	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("find databases: %w", err)
	}
	defer cursor.Close(ctx)

	var databases []DatabaseConfig
	if err := cursor.All(ctx, &databases); err != nil {
		return nil, fmt.Errorf("decode databases: %w", err)
	}

	return databases, nil
}

// GetDatabase retrieves a single database configuration by ID
func (s *Store) GetDatabase(ctx context.Context, id string) (*DatabaseConfig, error) {
	filter := bson.M{"_id": id}

	var db DatabaseConfig
	err := s.db.Collection("databases").FindOne(ctx, filter).Decode(&db)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("database not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("find database: %w", err)
	}

	return &db, nil
}

// SaveDatabase creates or updates a database configuration
func (s *Store) SaveDatabase(ctx context.Context, db DatabaseConfig) error {
	collection := s.db.Collection("databases")
	filter := bson.M{"_id": db.ID}
	update := bson.M{"$set": db}
	opts := options.Update().SetUpsert(true)

	_, err := collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("save database: %w", err)
	}

	return nil
}

// DeleteDatabase removes a database configuration
func (s *Store) DeleteDatabase(ctx context.Context, id string) error {
	filter := bson.M{"_id": id}

	result, err := s.db.Collection("databases").DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("delete database: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("database not found: %s", id)
	}

	return nil
}

// Retention Run Methods with Database Support

// ListRetentionRunsForDatabase retrieves retention runs for a specific database
func (s *Store) ListRetentionRunsForDatabase(ctx context.Context, databaseID string, limit, offset int) ([]RetentionRun, error) {
	collection := s.db.Collection("retention_runs")

	filter := bson.M{"database_id": databaseID}
	opts := options.Find().
		SetSort(bson.D{{"started_at", -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find retention runs: %w", err)
	}
	defer cursor.Close(ctx)

	var runs []RetentionRun
	if err := cursor.All(ctx, &runs); err != nil {
		return nil, fmt.Errorf("decode retention runs: %w", err)
	}

	return runs, nil
}

// Statistics Methods

// GetDatabaseStatistics retrieves statistics for a specific database
func (s *Store) GetDatabaseStatistics(ctx context.Context, databaseID string) (*DatabaseStatistics, error) {
	stats := &DatabaseStatistics{
		DatabaseID: databaseID,
	}

	// Count backup runs
	backupCollection := s.db.Collection("backup_runs")
	filter := bson.M{"database_id": databaseID}

	totalBackups, err := backupCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("count backups: %w", err)
	}
	stats.TotalBackups = int(totalBackups)

	// Count successful backups
	successFilter := bson.M{"database_id": databaseID, "status": "success"}
	successfulBackups, err := backupCollection.CountDocuments(ctx, successFilter)
	if err != nil {
		return nil, fmt.Errorf("count successful backups: %w", err)
	}
	stats.SuccessfulBackups = int(successfulBackups)

	// Count failed backups
	failedFilter := bson.M{"database_id": databaseID, "status": "failed"}
	failedBackups, err := backupCollection.CountDocuments(ctx, failedFilter)
	if err != nil {
		return nil, fmt.Errorf("count failed backups: %w", err)
	}
	stats.FailedBackups = int(failedBackups)

	// Get last backup time
	lastBackup, err := s.GetLatestBackupRunForDatabase(ctx, databaseID)
	if err == nil && lastBackup != nil {
		stats.LastBackupTime = &lastBackup.StartedAt
		stats.LastBackupStatus = lastBackup.Status
	}

	// Count restore runs
	restoreCollection := s.db.Collection("restore_runs")
	totalRestores, err := restoreCollection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("count restores: %w", err)
	}
	stats.TotalRestores = int(totalRestores)

	// Get last restore time
	lastRestore, err := s.GetLatestRestoreRunForDatabase(ctx, databaseID)
	if err == nil && lastRestore != nil {
		stats.LastRestoreTime = &lastRestore.StartedAt
		stats.LastRestoreStatus = lastRestore.Status
	}

	return stats, nil
}

// DatabaseStatistics represents statistics for a database
type DatabaseStatistics struct {
	DatabaseID        string     `json:"database_id"`
	TotalBackups      int        `json:"total_backups"`
	SuccessfulBackups int        `json:"successful_backups"`
	FailedBackups     int        `json:"failed_backups"`
	LastBackupTime    *time.Time `json:"last_backup_time,omitempty"`
	LastBackupStatus  string     `json:"last_backup_status,omitempty"`
	TotalRestores     int        `json:"total_restores"`
	LastRestoreTime   *time.Time `json:"last_restore_time,omitempty"`
	LastRestoreStatus string     `json:"last_restore_status,omitempty"`
}

// Audit Event Methods with Database Context

// ListAuditEventsForDatabase retrieves audit events for a specific database
func (s *Store) ListAuditEventsForDatabase(ctx context.Context, databaseID string, limit, offset int) ([]AuditEvent, error) {
	collection := s.db.Collection("audit_events")

	filter := bson.M{"metadata.database_id": databaseID}
	opts := options.Find().
		SetSort(bson.D{{"timestamp", -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find audit events: %w", err)
	}
	defer cursor.Close(ctx)

	var events []AuditEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, fmt.Errorf("decode audit events: %w", err)
	}

	return events, nil
}