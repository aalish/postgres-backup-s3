package store

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/argon2"
)

// User represents a system user account
type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Username     string             `bson:"username" json:"username"`
	Email        string             `bson:"email" json:"email"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Role         string             `bson:"role" json:"role"` // admin, operator, viewer
	Active       bool               `bson:"active" json:"active"`

	// Database access permissions
	AllowedDatabases []string `bson:"allowed_databases" json:"allowed_databases"` // Empty means all databases
	Permissions      UserPermissions `bson:"permissions" json:"permissions"`

	// Metadata
	CreatedAt    time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `bson:"updated_at" json:"updated_at"`
	LastLoginAt  *time.Time `bson:"last_login_at,omitempty" json:"last_login_at,omitempty"`
	PasswordChangedAt *time.Time `bson:"password_changed_at,omitempty" json:"password_changed_at,omitempty"`

	// Security
	MFAEnabled   bool   `bson:"mfa_enabled" json:"mfa_enabled"`
	MFASecret    string `bson:"mfa_secret,omitempty" json:"-"`
	APIToken     string `bson:"api_token,omitempty" json:"-"`
	APITokenHash string `bson:"api_token_hash,omitempty" json:"-"`
}

// UserPermissions defines granular permissions for a user
type UserPermissions struct {
	// Backup permissions
	ViewBackups    bool `bson:"view_backups" json:"view_backups"`
	CreateBackups  bool `bson:"create_backups" json:"create_backups"`
	DeleteBackups  bool `bson:"delete_backups" json:"delete_backups"`

	// Restore permissions
	ViewRestores   bool `bson:"view_restores" json:"view_restores"`
	CreateRestores bool `bson:"create_restores" json:"create_restores"`

	// Configuration permissions
	ViewSettings   bool `bson:"view_settings" json:"view_settings"`
	EditSettings   bool `bson:"edit_settings" json:"edit_settings"`

	// Database management
	ViewDatabases  bool `bson:"view_databases" json:"view_databases"`
	EditDatabases  bool `bson:"edit_databases" json:"edit_databases"`

	// User management (admin only)
	ManageUsers    bool `bson:"manage_users" json:"manage_users"`
	ViewAuditLogs  bool `bson:"view_audit_logs" json:"view_audit_logs"`
}

// DefaultPermissionsByRole returns default permissions for a role
func DefaultPermissionsByRole(role string) UserPermissions {
	switch role {
	case "admin":
		return UserPermissions{
			ViewBackups:    true,
			CreateBackups:  true,
			DeleteBackups:  true,
			ViewRestores:   true,
			CreateRestores: true,
			ViewSettings:   true,
			EditSettings:   true,
			ViewDatabases:  true,
			EditDatabases:  true,
			ManageUsers:    true,
			ViewAuditLogs:  true,
		}
	case "operator":
		return UserPermissions{
			ViewBackups:    true,
			CreateBackups:  true,
			DeleteBackups:  false,
			ViewRestores:   true,
			CreateRestores: true,
			ViewSettings:   true,
			EditSettings:   false,
			ViewDatabases:  true,
			EditDatabases:  false,
			ManageUsers:    false,
			ViewAuditLogs:  true,
		}
	case "viewer":
		return UserPermissions{
			ViewBackups:    true,
			CreateBackups:  false,
			DeleteBackups:  false,
			ViewRestores:   true,
			CreateRestores: false,
			ViewSettings:   true,
			EditSettings:   false,
			ViewDatabases:  true,
			EditDatabases:  false,
			ManageUsers:    false,
			ViewAuditLogs:  false,
		}
	default:
		// Minimal permissions by default
		return UserPermissions{
			ViewBackups:   true,
			ViewRestores:  true,
			ViewDatabases: true,
		}
	}
}

// Password hashing parameters
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword creates a secure hash of the password using Argon2
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	// Encode salt and hash together
	combined := make([]byte, saltLen+argonKeyLen)
	copy(combined, salt)
	copy(combined[saltLen:], hash)

	return hex.EncodeToString(combined), nil
}

// VerifyPassword checks if the password matches the hash
func VerifyPassword(password, hashStr string) (bool, error) {
	combined, err := hex.DecodeString(hashStr)
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}

	if len(combined) != saltLen+argonKeyLen {
		return false, errors.New("invalid hash length")
	}

	salt := combined[:saltLen]
	hash := combined[saltLen:]

	testHash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return subtle.ConstantTimeCompare(hash, testHash) == 1, nil
}

// CreateUser adds a new user to the database
func (s *Store) CreateUser(ctx context.Context, user *User) error {
	user.ID = primitive.NewObjectID()
	user.CreatedAt = time.Now()
	user.UpdatedAt = user.CreatedAt
	user.Active = true

	// Set default permissions based on role
	if user.Role != "" && len(user.Permissions.getAllPermissions()) == 0 {
		user.Permissions = DefaultPermissionsByRole(user.Role)
	}

	// Ensure username is unique
	existing, _ := s.GetUserByUsername(ctx, user.Username)
	if existing != nil {
		return fmt.Errorf("username already exists: %s", user.Username)
	}

	// Ensure email is unique
	if user.Email != "" {
		existing, _ = s.GetUserByEmail(ctx, user.Email)
		if existing != nil {
			return fmt.Errorf("email already exists: %s", user.Email)
		}
	}

	_, err := s.users.InsertOne(ctx, user)
	return err
}

// GetUserByUsername retrieves a user by username
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	err := s.users.FindOne(ctx, bson.M{"username": username, "active": true}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found: %s", username)
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by email
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := s.users.FindOne(ctx, bson.M{"email": email, "active": true}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found with email: %s", email)
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByID retrieves a user by ID
func (s *Store) GetUserByID(ctx context.Context, id primitive.ObjectID) (*User, error) {
	var user User
	err := s.users.FindOne(ctx, bson.M{"_id": id, "active": true}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &user, nil
}

// UpdateUser updates user information
func (s *Store) UpdateUser(ctx context.Context, id primitive.ObjectID, update bson.M) error {
	update["updated_at"] = time.Now()

	result, err := s.users.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": update},
	)

	if err != nil {
		return err
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// UpdateUserPassword updates a user's password
func (s *Store) UpdateUserPassword(ctx context.Context, id primitive.ObjectID, newPasswordHash string) error {
	now := time.Now()
	return s.UpdateUser(ctx, id, bson.M{
		"password_hash": newPasswordHash,
		"password_changed_at": now,
	})
}

// UpdateLastLogin updates the last login timestamp
func (s *Store) UpdateLastLogin(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	return s.UpdateUser(ctx, id, bson.M{"last_login_at": now})
}

// ListUsers returns all active users
func (s *Store) ListUsers(ctx context.Context, filter bson.M, opts ...*options.FindOptions) ([]*User, error) {
	if filter == nil {
		filter = bson.M{}
	}
	filter["active"] = true

	cursor, err := s.users.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []*User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}

	return users, nil
}

// DeleteUser soft deletes a user by marking as inactive
func (s *Store) DeleteUser(ctx context.Context, id primitive.ObjectID) error {
	return s.UpdateUser(ctx, id, bson.M{"active": false})
}

// HardDeleteUser permanently removes a user
func (s *Store) HardDeleteUser(ctx context.Context, id primitive.ObjectID) error {
	result, err := s.users.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// Helper method to check if user has any permissions set
func (p UserPermissions) getAllPermissions() []bool {
	return []bool{
		p.ViewBackups, p.CreateBackups, p.DeleteBackups,
		p.ViewRestores, p.CreateRestores,
		p.ViewSettings, p.EditSettings,
		p.ViewDatabases, p.EditDatabases,
		p.ManageUsers, p.ViewAuditLogs,
	}
}

// CanAccessDatabase checks if user can access a specific database
func (u *User) CanAccessDatabase(databaseID string) bool {
	// Admin can access all databases
	if u.Role == "admin" {
		return true
	}

	// If no specific databases are set, user can access all
	if len(u.AllowedDatabases) == 0 {
		return true
	}

	// Check if database is in allowed list
	for _, allowed := range u.AllowedDatabases {
		if allowed == databaseID || allowed == "*" {
			return true
		}
	}

	return false
}

// GenerateAPIToken generates a new API token for the user
func (u *User) GenerateAPIToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	token := hex.EncodeToString(tokenBytes)
	hash, err := HashPassword(token) // Reuse password hashing for token
	if err != nil {
		return "", fmt.Errorf("hash token: %w", err)
	}

	u.APIToken = token
	u.APITokenHash = hash

	return token, nil
}

// VerifyAPIToken verifies an API token
func (s *Store) VerifyAPIToken(ctx context.Context, token string) (*User, error) {
	// First, try to find user with this exact token (for migration)
	var user User
	err := s.users.FindOne(ctx, bson.M{"api_token": token, "active": true}).Decode(&user)
	if err == nil {
		return &user, nil
	}

	// Otherwise, check all users' token hashes
	users, err := s.ListUsers(ctx, nil)
	if err != nil {
		return nil, err
	}

	for _, u := range users {
		if u.APITokenHash != "" {
			match, err := VerifyPassword(token, u.APITokenHash)
			if err == nil && match {
				return u, nil
			}
		}
	}

	return nil, fmt.Errorf("invalid API token")
}