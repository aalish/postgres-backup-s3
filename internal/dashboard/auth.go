package dashboard

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/neelgai/postgres-backup/internal/config"
	"github.com/neelgai/postgres-backup/internal/store"
)

const (
	accessCookieName    = "pgbackup_access"
	refreshCookieName   = "pgbackup_refresh"
	loginCSRFCookieName = "pgbackup_login_csrf"
)

var (
	errUnauthorized = errors.New("unauthorized")
	errRateLimited  = errors.New("rate limited")
	errInvalidCSRF  = errors.New("invalid csrf token")
	errTokenExpired = errors.New("token expired")
)

type authContextKey string

const userContextKey authContextKey = "dashboard-user"

type UserSession struct {
	Username  string
	SessionID string
	CSRFToken string
}

type AuthManager struct {
	cfg          config.AuthConfig
	cookieSecure bool
	store        *store.Store
	logger       *slog.Logger
	limiter      *loginRateLimiter
}

type tokenClaims struct {
	Sub  string `json:"sub"`
	SID  string `json:"sid"`
	JTI  string `json:"jti,omitempty"`
	Type string `json:"type"`
	IAT  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type loginRateLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string]rateAttempt
}

type rateAttempt struct {
	Count   int
	ResetAt time.Time
}

func NewAuthManager(cfg config.AuthConfig, cookieSecure bool, store *store.Store, logger *slog.Logger) *AuthManager {
	return &AuthManager{
		cfg:          cfg,
		cookieSecure: cookieSecure,
		store:        store,
		logger:       logger,
		limiter: &loginRateLimiter{
			limit:    cfg.LoginRateLimit,
			window:   cfg.LoginRateWindow,
			attempts: make(map[string]rateAttempt),
		},
	}
}

func (a *AuthManager) EnsureLoginCSRF(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(loginCSRFCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value
	}

	token := store.NewID()
	http.SetCookie(w, &http.Cookie{
		Name:     loginCSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((30 * time.Minute).Seconds()),
	})
	return token
}

func (a *AuthManager) ValidateLoginCSRF(r *http.Request) error {
	cookie, err := r.Cookie(loginCSRFCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return errInvalidCSRF
	}
	if err := r.ParseForm(); err != nil {
		return errInvalidCSRF
	}
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.FormValue("_csrf"))) != 1 {
		return errInvalidCSRF
	}
	return nil
}

func (a *AuthManager) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, username, password string) (UserSession, error) {
	clientKey := clientAddress(r)
	if allowed, _ := a.limiter.allow(clientKey); !allowed {
		return UserSession{}, errRateLimited
	}

	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte(a.cfg.AdminUsername)) != 1 ||
		subtle.ConstantTimeCompare([]byte(password), []byte(a.cfg.AdminPassword)) != 1 {
		a.limiter.registerFailure(clientKey)
		return UserSession{}, errUnauthorized
	}

	a.limiter.reset(clientKey)

	now := time.Now().UTC()
	sessionID := store.NewID()
	refreshJTI := store.NewID()
	csrfToken := store.NewID()
	session := store.Session{
		ID:               sessionID,
		Username:         a.cfg.AdminUsername,
		RefreshTokenHash: hashValue(refreshJTI),
		CSRFToken:        csrfToken,
		ExpiresAt:        now.Add(a.cfg.RefreshTokenTTL),
		CreatedAt:        now,
		UpdatedAt:        now,
		LastSeenAt:       now,
	}
	if err := a.store.CreateSession(ctx, session); err != nil {
		return UserSession{}, err
	}

	if err := a.issueCookies(w, session, refreshJTI); err != nil {
		return UserSession{}, err
	}
	a.clearLoginCSRFCookie(w)

	return UserSession{
		Username:  session.Username,
		SessionID: session.ID,
		CSRFToken: session.CSRFToken,
	}, nil
}

func (a *AuthManager) AuthenticateRequest(w http.ResponseWriter, r *http.Request) (UserSession, error) {
	if session, err := a.fromAccessToken(r); err == nil {
		return session, nil
	}

	refreshCookie, err := r.Cookie(refreshCookieName)
	if err != nil || strings.TrimSpace(refreshCookie.Value) == "" {
		return UserSession{}, errUnauthorized
	}

	claims, err := a.parseToken(refreshCookie.Value)
	if err != nil {
		return UserSession{}, errUnauthorized
	}
	if claims.Type != "refresh" || claims.JTI == "" {
		return UserSession{}, errUnauthorized
	}

	sessionDoc, err := a.store.GetSession(r.Context(), claims.SID)
	if err != nil {
		return UserSession{}, errUnauthorized
	}
	if sessionRevoked(sessionDoc) || sessionExpired(sessionDoc, time.Now().UTC()) {
		return UserSession{}, errUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(sessionDoc.RefreshTokenHash), []byte(hashValue(claims.JTI))) != 1 {
		return UserSession{}, errUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(sessionDoc.Username), []byte(claims.Sub)) != 1 {
		return UserSession{}, errUnauthorized
	}

	newJTI := store.NewID()
	now := time.Now().UTC()
	sessionDoc.RefreshTokenHash = hashValue(newJTI)
	sessionDoc.ExpiresAt = now.Add(a.cfg.RefreshTokenTTL)
	sessionDoc.UpdatedAt = now
	sessionDoc.LastSeenAt = now
	if err := a.store.SaveSession(r.Context(), sessionDoc); err != nil {
		return UserSession{}, err
	}
	if err := a.issueCookies(w, sessionDoc, newJTI); err != nil {
		return UserSession{}, err
	}

	return UserSession{
		Username:  sessionDoc.Username,
		SessionID: sessionDoc.ID,
		CSRFToken: sessionDoc.CSRFToken,
	}, nil
}

func (a *AuthManager) VerifyCSRF(r *http.Request, user UserSession) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return nil
	}
	token := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	if token == "" {
		if err := r.ParseForm(); err == nil {
			token = strings.TrimSpace(r.FormValue("_csrf"))
		}
	}
	if token == "" {
		return errInvalidCSRF
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(user.CSRFToken)) != 1 {
		return errInvalidCSRF
	}
	return nil
}

func (a *AuthManager) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if sessionID := a.sessionIDFromRequest(r); sessionID != "" {
		if err := a.store.RevokeSession(ctx, sessionID, time.Now().UTC()); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	a.clearAuthCookies(w)
	a.clearLoginCSRFCookie(w)
	return nil
}

func (a *AuthManager) fromAccessToken(r *http.Request) (UserSession, error) {
	accessCookie, err := r.Cookie(accessCookieName)
	if err != nil || strings.TrimSpace(accessCookie.Value) == "" {
		return UserSession{}, errUnauthorized
	}

	claims, err := a.parseToken(accessCookie.Value)
	if err != nil {
		return UserSession{}, err
	}
	if claims.Type != "access" {
		return UserSession{}, errUnauthorized
	}

	sessionDoc, err := a.store.GetSession(r.Context(), claims.SID)
	if err != nil {
		return UserSession{}, errUnauthorized
	}
	now := time.Now().UTC()
	if sessionRevoked(sessionDoc) || sessionExpired(sessionDoc, now) {
		return UserSession{}, errUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(sessionDoc.Username), []byte(claims.Sub)) != 1 {
		return UserSession{}, errUnauthorized
	}

	return UserSession{
		Username:  sessionDoc.Username,
		SessionID: sessionDoc.ID,
		CSRFToken: sessionDoc.CSRFToken,
	}, nil
}

func (a *AuthManager) issueCookies(w http.ResponseWriter, session store.Session, refreshJTI string) error {
	now := time.Now().UTC()
	accessToken, err := a.signToken(tokenClaims{
		Sub:  session.Username,
		SID:  session.ID,
		Type: "access",
		IAT:  now.Unix(),
		Exp:  now.Add(a.cfg.AccessTokenTTL).Unix(),
	})
	if err != nil {
		return err
	}
	refreshToken, err := a.signToken(tokenClaims{
		Sub:  session.Username,
		SID:  session.ID,
		JTI:  refreshJTI,
		Type: "refresh",
		IAT:  now.Unix(),
		Exp:  now.Add(a.cfg.RefreshTokenTTL).Unix(),
	})
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(a.cfg.AccessTokenTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(a.cfg.RefreshTokenTTL.Seconds()),
	})

	return nil
}

func (a *AuthManager) signToken(claims tokenClaims) (string, error) {
	headerBytes, err := json.Marshal(jwtHeader{Alg: "HS256", Typ: "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshal JWT header: %w", err)
	}
	claimsBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal JWT claims: %w", err)
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(headerBytes)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsBytes)
	unsigned := encodedHeader + "." + encodedClaims

	signature := hmac.New(sha256.New, []byte(a.cfg.JWTSecret))
	if _, err := signature.Write([]byte(unsigned)); err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil)), nil
}

func (a *AuthManager) parseToken(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errUnauthorized
	}

	var header jwtHeader
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return tokenClaims{}, errUnauthorized
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return tokenClaims{}, errUnauthorized
	}
	if header.Alg != "HS256" || header.Typ != "JWT" {
		return tokenClaims{}, errUnauthorized
	}

	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(a.cfg.JWTSecret))
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		return tokenClaims{}, errUnauthorized
	}
	expectedSig := mac.Sum(nil)
	receivedSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return tokenClaims{}, errUnauthorized
	}
	if !hmac.Equal(expectedSig, receivedSig) {
		return tokenClaims{}, errUnauthorized
	}

	var claims tokenClaims
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, errUnauthorized
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return tokenClaims{}, errUnauthorized
	}
	if claims.Exp <= time.Now().UTC().Unix() {
		return tokenClaims{}, errTokenExpired
	}

	return claims, nil
}

func (a *AuthManager) clearAuthCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (a *AuthManager) clearLoginCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     loginCSRFCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (a *AuthManager) sessionIDFromRequest(r *http.Request) string {
	if accessCookie, err := r.Cookie(accessCookieName); err == nil && strings.TrimSpace(accessCookie.Value) != "" {
		if claims, err := a.parseToken(accessCookie.Value); err == nil {
			return claims.SID
		}
	}
	if refreshCookie, err := r.Cookie(refreshCookieName); err == nil && strings.TrimSpace(refreshCookie.Value) != "" {
		if claims, err := a.parseToken(refreshCookie.Value); err == nil {
			return claims.SID
		}
	}
	return ""
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sessionRevoked(session store.Session) bool {
	return session.RevokedAt != nil
}

func sessionExpired(session store.Session, now time.Time) bool {
	return !session.ExpiresAt.IsZero() && session.ExpiresAt.Before(now)
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *loginRateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	attempt := l.attempts[key]
	now := time.Now().UTC()
	if attempt.ResetAt.IsZero() || now.After(attempt.ResetAt) {
		delete(l.attempts, key)
		return true, 0
	}
	if attempt.Count >= l.limit {
		return false, time.Until(attempt.ResetAt)
	}

	return true, 0
}

func (l *loginRateLimiter) registerFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	attempt := l.attempts[key]
	if attempt.ResetAt.IsZero() || now.After(attempt.ResetAt) {
		attempt = rateAttempt{ResetAt: now.Add(l.window)}
	}
	attempt.Count++
	l.attempts[key] = attempt
}

func (l *loginRateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.attempts, key)
}
