package config

import (
	"errors"
	"os"
	"strings"
	"time"
)

type DashboardConfig struct {
	Base     Config
	HTTP     HTTPConfig
	Auth     AuthConfig
	Mongo    MongoConfig
	Defaults DefaultSettingsConfig
}

type HTTPConfig struct {
	Addr            string
	CookieSecure    bool
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

type AuthConfig struct {
	AdminUsername   string
	AdminPassword   string
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	LoginRateLimit  int
	LoginRateWindow time.Duration
}

type MongoConfig struct {
	URI            string
	Database       string
	ConnectTimeout time.Duration
}

type DefaultSettingsConfig struct {
	RetentionDays      int
	BackupSchedule     string
	NotificationEnable bool
	WebhookURL         string
	WebhookTimeout     time.Duration
}

func LoadDashboard() (DashboardConfig, error) {
	base, err := Load()
	if err != nil {
		return DashboardConfig{}, err
	}

	cookieSecure, err := readBool("COOKIE_SECURE", true)
	if err != nil {
		return DashboardConfig{}, err
	}
	readTimeout, err := readDuration("HTTP_READ_TIMEOUT", 10*time.Second)
	if err != nil {
		return DashboardConfig{}, err
	}
	writeTimeout, err := readDuration("HTTP_WRITE_TIMEOUT", 30*time.Second)
	if err != nil {
		return DashboardConfig{}, err
	}
	idleTimeout, err := readDuration("HTTP_IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return DashboardConfig{}, err
	}
	shutdownTimeout, err := readDuration("HTTP_SHUTDOWN_TIMEOUT", 30*time.Second)
	if err != nil {
		return DashboardConfig{}, err
	}
	accessTokenTTL, err := readDuration("AUTH_ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return DashboardConfig{}, err
	}
	refreshTokenTTL, err := readDuration("AUTH_REFRESH_TOKEN_TTL", 7*24*time.Hour)
	if err != nil {
		return DashboardConfig{}, err
	}
	loginRateLimit, err := readInt("AUTH_LOGIN_RATE_LIMIT", 5)
	if err != nil {
		return DashboardConfig{}, err
	}
	loginRateWindow, err := readDuration("AUTH_LOGIN_RATE_WINDOW", 15*time.Minute)
	if err != nil {
		return DashboardConfig{}, err
	}
	connectTimeout, err := readDuration("MONGODB_CONNECT_TIMEOUT", 10*time.Second)
	if err != nil {
		return DashboardConfig{}, err
	}
	retentionDays, err := readInt("DEFAULT_RETENTION_DAYS", 30)
	if err != nil {
		return DashboardConfig{}, err
	}
	notificationEnable, err := readBool("DEFAULT_NOTIFICATION_ENABLED", false)
	if err != nil {
		return DashboardConfig{}, err
	}
	webhookTimeout, err := readDuration("DEFAULT_NOTIFICATION_WEBHOOK_TIMEOUT", 10*time.Second)
	if err != nil {
		return DashboardConfig{}, err
	}

	cfg := DashboardConfig{
		Base: base,
		HTTP: HTTPConfig{
			Addr:            getEnv("HTTP_ADDR", ":8080"),
			CookieSecure:    cookieSecure,
			ReadTimeout:     readTimeout,
			WriteTimeout:    writeTimeout,
			IdleTimeout:     idleTimeout,
			ShutdownTimeout: shutdownTimeout,
		},
		Auth: AuthConfig{
			AdminUsername:   strings.TrimSpace(getEnv("ADMIN_USERNAME", "admin")),
			AdminPassword:   os.Getenv("ADMIN_PASSWORD"),
			JWTSecret:       os.Getenv("JWT_SECRET"),
			AccessTokenTTL:  accessTokenTTL,
			RefreshTokenTTL: refreshTokenTTL,
			LoginRateLimit:  loginRateLimit,
			LoginRateWindow: loginRateWindow,
		},
		Mongo: MongoConfig{
			URI:            strings.TrimSpace(os.Getenv("MONGODB_URI")),
			Database:       strings.TrimSpace(getEnv("MONGODB_DATABASE", "pgbackup")),
			ConnectTimeout: connectTimeout,
		},
		Defaults: DefaultSettingsConfig{
			RetentionDays:      retentionDays,
			BackupSchedule:     getEnv("DEFAULT_BACKUP_SCHEDULE", "0 2 * * *"),
			NotificationEnable: notificationEnable,
			WebhookURL:         strings.TrimSpace(os.Getenv("DEFAULT_NOTIFICATION_WEBHOOK_URL")),
			WebhookTimeout:     webhookTimeout,
		},
	}

	if err := cfg.Validate(); err != nil {
		return DashboardConfig{}, err
	}

	return cfg, nil
}

func (c DashboardConfig) Validate() error {
	var problems []string

	if strings.TrimSpace(c.HTTP.Addr) == "" {
		problems = append(problems, "HTTP_ADDR is required")
	}
	if c.HTTP.ReadTimeout <= 0 {
		problems = append(problems, "HTTP_READ_TIMEOUT must be greater than 0")
	}
	if c.HTTP.WriteTimeout <= 0 {
		problems = append(problems, "HTTP_WRITE_TIMEOUT must be greater than 0")
	}
	if c.HTTP.IdleTimeout <= 0 {
		problems = append(problems, "HTTP_IDLE_TIMEOUT must be greater than 0")
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		problems = append(problems, "HTTP_SHUTDOWN_TIMEOUT must be greater than 0")
	}
	if strings.TrimSpace(c.Auth.AdminUsername) == "" {
		problems = append(problems, "ADMIN_USERNAME is required")
	}
	if c.Auth.AdminPassword == "" {
		problems = append(problems, "ADMIN_PASSWORD is required")
	}
	if len(c.Auth.JWTSecret) < 32 {
		problems = append(problems, "JWT_SECRET must be at least 32 characters long")
	}
	if c.Auth.AccessTokenTTL <= 0 {
		problems = append(problems, "AUTH_ACCESS_TOKEN_TTL must be greater than 0")
	}
	if c.Auth.RefreshTokenTTL <= 0 {
		problems = append(problems, "AUTH_REFRESH_TOKEN_TTL must be greater than 0")
	}
	if c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL {
		problems = append(problems, "AUTH_REFRESH_TOKEN_TTL must be greater than AUTH_ACCESS_TOKEN_TTL")
	}
	if c.Auth.LoginRateLimit < 1 {
		problems = append(problems, "AUTH_LOGIN_RATE_LIMIT must be at least 1")
	}
	if c.Auth.LoginRateWindow <= 0 {
		problems = append(problems, "AUTH_LOGIN_RATE_WINDOW must be greater than 0")
	}
	if strings.TrimSpace(c.Mongo.URI) == "" {
		problems = append(problems, "MONGODB_URI is required")
	}
	if strings.TrimSpace(c.Mongo.Database) == "" {
		problems = append(problems, "MONGODB_DATABASE is required")
	}
	if c.Mongo.ConnectTimeout <= 0 {
		problems = append(problems, "MONGODB_CONNECT_TIMEOUT must be greater than 0")
	}
	if c.Defaults.RetentionDays < 1 {
		problems = append(problems, "DEFAULT_RETENTION_DAYS must be at least 1")
	}
	if strings.TrimSpace(c.Defaults.BackupSchedule) == "" {
		problems = append(problems, "DEFAULT_BACKUP_SCHEDULE is required")
	}
	if c.Defaults.WebhookTimeout <= 0 {
		problems = append(problems, "DEFAULT_NOTIFICATION_WEBHOOK_TIMEOUT must be greater than 0")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}

	return nil
}
