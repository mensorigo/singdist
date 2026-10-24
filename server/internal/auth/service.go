// Package auth owns admin accounts, first-run bootstrap, login rate
// limiting and cookie sessions. It depends only on the Repository interface
// below (satisfied by internal/store) and never touches SQL itself.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// CookieName is the session cookie set by the login handler and read by the
// auth guard middleware (wired in main via httpx).
const CookieName = "singbox-ui_session"

// SessionTTL is the lifetime of a login session.
const SessionTTL = 7 * 24 * time.Hour

const (
	bcryptCost     = 10
	tokenBytes     = 32
	passwordLen    = 16
	maxFailures    = 5
	lockoutWindow  = 5 * time.Minute
	defaultAdmin   = "singbox-ui"
	passwordRunes  = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789" // unambiguous: no 0/O/1/l/I/o
	sessionHashLen = sha256.Size
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrRateLimited        = errors.New("auth: too many failed login attempts")
	ErrInvalidSession     = errors.New("auth: invalid session")
	ErrWrongPassword      = errors.New("auth: current password incorrect")
	ErrInvalidUsername    = errors.New("auth: invalid username")
	ErrUsernameTaken      = errors.New("auth: username already taken")
	ErrInvalidPassword    = errors.New("auth: invalid password")
)

// Repository is the persistence seam auth needs from the store layer.
type Repository interface {
	CountAdmins(ctx context.Context) (int, error)
	CreateAdmin(ctx context.Context, username, passwordHash string, now int64) (int64, error)
	AdminCredentials(ctx context.Context, username string) (id int64, passwordHash string, ok bool, err error)
	AdminUsername(ctx context.Context, id int64) (string, bool, error)
	UpdateAdminPassword(ctx context.Context, id int64, passwordHash string, now int64) error
	UpdateAdminUsername(ctx context.Context, id int64, username string, now int64) error
	CreateSession(ctx context.Context, tokenHash string, adminID, expiresAt, now int64) error
	SessionAdmin(ctx context.Context, tokenHash string) (adminID int64, expiresAt int64, ok bool, err error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteOtherSessions(ctx context.Context, adminID int64, exceptTokenHash string) (int64, error)
	DeleteExpiredSessions(ctx context.Context, now int64) error
}

// Service implements all authentication behaviour.
type Service struct {
	repo    Repository
	limiter *loginLimiter
}

// NewService creates an auth Service on top of repo.
func NewService(repo Repository) *Service {
	return &Service{repo: repo, limiter: newLoginLimiter()}
}

// Bootstrap creates the initial admin account when none exists. It returns
// the generated plaintext password exactly once (on the first run only);
// the caller is responsible for printing it to the operator. Afterwards
// only the bcrypt hash exists anywhere in the system.
func (s *Service) Bootstrap(ctx context.Context) (created bool, username, password string, err error) {
	count, err := s.repo.CountAdmins(ctx)
	if err != nil {
		return false, "", "", err
	}
	if count > 0 {
		return false, "", "", nil
	}
	password, err = randomPassword(passwordLen)
	if err != nil {
		return false, "", "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return false, "", "", err
	}
	if _, err := s.repo.CreateAdmin(ctx, defaultAdmin, string(hash), time.Now().Unix()); err != nil {
		return false, "", "", err
	}
	return true, defaultAdmin, password, nil
}

// Login verifies credentials and, on success, mints a session. The returned
// token is the raw cookie value; only its SHA-256 hash is stored.
func (s *Service) Login(ctx context.Context, username, password, clientIP string) (token string, expiresAt time.Time, err error) {
	key := username + "|" + clientIP
	if s.limiter.Locked(key) {
		return "", time.Time{}, ErrRateLimited
	}
	id, hash, ok, err := s.repo.AdminCredentials(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	// Compare against a dummy hash for unknown users to keep the timing
	// profile similar and avoid leaking which usernames exist.
	if !ok {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		s.limiter.Fail(key)
		return "", time.Time{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		s.limiter.Fail(key)
		return "", time.Time{}, ErrInvalidCredentials
	}
	s.limiter.Succeed(key)

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token = hex.EncodeToString(raw)
	now := time.Now()
	expires := now.Add(SessionTTL)
	if err := s.repo.CreateSession(ctx, hashToken(token), id, expires.Unix(), now.Unix()); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// ValidateSession implements the TokenValidator contract used by the HTTP
// auth guard: it resolves a raw cookie token to the admin identity.
func (s *Service) ValidateSession(ctx context.Context, token string) (int64, string, error) {
	if token == "" {
		return 0, "", ErrInvalidSession
	}
	adminID, expiresAt, ok, err := s.repo.SessionAdmin(ctx, hashToken(token))
	if err != nil {
		return 0, "", err
	}
	if !ok || time.Now().Unix() > expiresAt {
		return 0, "", ErrInvalidSession
	}
	username, ok, err := s.repo.AdminUsername(ctx, adminID)
	if err != nil {
		return 0, "", err
	}
	if !ok {
		return 0, "", ErrInvalidSession
	}
	return adminID, username, nil
}

// SessionHash exposes the stored form of a session token (used by the guard
// to make the current session available to handlers such as logout).
func SessionHash(token string) string { return hashToken(token) }

// Logout deletes the session for the given raw token, if any.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.repo.DeleteSession(ctx, hashToken(token))
}

// checkPassword verifies a password for an admin id, without leaking
// whether the admin exists.
func (s *Service) checkPassword(ctx context.Context, adminID int64, password string) error {
	username, ok, err := s.repo.AdminUsername(ctx, adminID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidSession
	}
	_, hash, ok, err := s.repo.AdminCredentials(ctx, username)
	if err != nil {
		return err
	}
	if !ok || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return ErrWrongPassword
	}
	return nil
}

// ChangePassword verifies the current password and replaces it.
func (s *Service) ChangePassword(ctx context.Context, adminID int64, current, next string) error {
	if err := s.checkPassword(ctx, adminID, current); err != nil {
		return err
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(next), bcryptCost)
	if err != nil {
		return err
	}
	return s.repo.UpdateAdminPassword(ctx, adminID, string(newHash), time.Now().Unix())
}

// ChangeUsername renames an admin. The current password must be supplied
// as proof of identity, and the new name must be unused. Sessions are
// bound to the admin id, so a rename keeps every live session valid —
// including the one making the request.
func (s *Service) ChangeUsername(ctx context.Context, adminID int64, currentPassword, newUsername string) (string, error) {
	if err := s.checkPassword(ctx, adminID, currentPassword); err != nil {
		return "", err
	}
	username := strings.TrimSpace(newUsername)
	if len(username) < 1 || len(username) > 64 || strings.ContainsFunc(username, unicode.IsSpace) {
		return "", ErrInvalidUsername
	}
	if id, _, ok, err := s.repo.AdminCredentials(ctx, username); err != nil {
		return "", err
	} else if ok && id != adminID {
		return "", ErrUsernameTaken
	}
	if err := s.repo.UpdateAdminUsername(ctx, adminID, username, time.Now().Unix()); err != nil {
		return "", err
	}
	return username, nil
}

// ChangeAccount applies a username and/or password change in one step,
// verified by a single current-password check. A nil newUsername or a
// nil/empty newPassword leaves that field untouched, so the form can
// submit either change alone or both together. It returns the admin's
// username after the change. A password change here is a security event
// exactly like ChangePassword; the caller revokes other sessions.
func (s *Service) ChangeAccount(ctx context.Context, adminID int64, currentPassword string, newUsername, newPassword *string) (string, error) {
	if err := s.checkPassword(ctx, adminID, currentPassword); err != nil {
		return "", err
	}
	username, ok, err := s.repo.AdminUsername(ctx, adminID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrInvalidSession
	}
	if newUsername != nil {
		name := strings.TrimSpace(*newUsername)
		if len(name) < 1 || len(name) > 64 || strings.ContainsFunc(name, unicode.IsSpace) {
			return "", ErrInvalidUsername
		}
		if name != username {
			if id, _, ok, err := s.repo.AdminCredentials(ctx, name); err != nil {
				return "", err
			} else if ok && id != adminID {
				return "", ErrUsernameTaken
			}
			if err := s.repo.UpdateAdminUsername(ctx, adminID, name, time.Now().Unix()); err != nil {
				return "", err
			}
			username = name
		}
	}
	if newPassword != nil && *newPassword != "" {
		if len(*newPassword) < 8 || len(*newPassword) > 72 {
			return "", ErrInvalidPassword
		}
		newHash, err := bcrypt.GenerateFromPassword([]byte(*newPassword), bcryptCost)
		if err != nil {
			return "", err
		}
		if err := s.repo.UpdateAdminPassword(ctx, adminID, string(newHash), time.Now().Unix()); err != nil {
			return "", err
		}
	}
	return username, nil
}

// RevokeOtherSessions signs out every session of an admin except the
// given one, and returns how many were removed.
func (s *Service) RevokeOtherSessions(ctx context.Context, adminID int64, exceptTokenHash string) (int64, error) {
	return s.repo.DeleteOtherSessions(ctx, adminID, exceptTokenHash)
}

// PruneSessions drops expired sessions; call periodically from main.
func (s *Service) PruneSessions(ctx context.Context) error {
	return s.repo.DeleteExpiredSessions(ctx, time.Now().Unix())
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomPassword(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(passwordRunes)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = passwordRunes[idx.Int64()]
	}
	return string(out), nil
}

// dummyHash is a precomputed bcrypt hash used only for timing equalisation.
var dummyHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	return h
}()

// --- login rate limiter -----------------------------------------------------

type attempt struct {
	failures    int
	lockedUntil time.Time
	lastFail    time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]*attempt)}
}

func (l *loginLimiter) Locked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[key]
	return ok && time.Now().Before(a.lockedUntil)
}

func (l *loginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a, ok := l.attempts[key]
	if !ok || now.Sub(a.lastFail) > lockoutWindow {
		a = &attempt{}
		l.attempts[key] = a
	}
	a.failures++
	a.lastFail = now
	if a.failures >= maxFailures {
		a.lockedUntil = now.Add(lockoutWindow)
		a.failures = 0
	}
}

func (l *loginLimiter) Succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
