package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type contextKey string

const (
	// AuthTypeKey is the context key holding the authentication method used.
	AuthTypeKey contextKey = "auth_type"
	// AuthTypeAPIKey means the request was authenticated via X-API-Key (bot/service).
	AuthTypeAPIKey = "api-key"
	// AuthTypeJWT means the request was authenticated via JWT Bearer token (admin UI).
	AuthTypeJWT = "jwt"
)

// User represents a stored admin user.
type User struct {
	Username string `json:"username"`
	Hash     string `json:"hash"` // bcrypt hash
}

// Auth provides JWT-based authentication backed by a users.json file.
type Auth struct {
	mu        sync.RWMutex
	users     []User
	filePath  string
	jwtSecret []byte
	apiKey    string // shared secret for service-to-service calls
}

// New loads users from filePath (creates the file if missing) and generates a JWT secret.
// apiKey is the shared secret for service-to-service authentication (X-API-Key header).
func New(filePath, apiKey string) (*Auth, error) {
	a := &Auth{filePath: filePath, apiKey: apiKey}

	// Generate a random JWT signing key on startup.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate jwt secret: %w", err)
	}
	a.jwtSecret = secret

	if err := a.load(); err != nil {
		return nil, err
	}

	return a, nil
}

// load reads the users file from disk.
func (a *Auth) load() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := os.ReadFile(a.filePath)
	if errors.Is(err, os.ErrNotExist) {
		a.users = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("read users file: %w", err)
	}

	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("parse users file: %w", err)
	}

	a.users = users
	return nil
}

// save persists the users list to disk.
func (a *Auth) save() error {
	data, err := json.MarshalIndent(a.users, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.filePath, data, 0600)
}

// AddUser creates or updates a user with a bcrypt-hashed password.
func (a *Auth) AddUser(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	for i, u := range a.users {
		if u.Username == username {
			a.users[i].Hash = string(hash)
			return a.save()
		}
	}

	a.users = append(a.users, User{Username: username, Hash: string(hash)})
	return a.save()
}

// Validate checks username/password against stored users.
func (a *Auth) Validate(username, password string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, u := range a.users {
		if u.Username == username {
			return bcrypt.CompareHashAndPassword([]byte(u.Hash), []byte(password)) == nil
		}
	}
	return false
}

// HasUsers returns true if at least one user exists.
func (a *Auth) HasUsers() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.users) > 0
}

// IssueToken creates a signed JWT for the given username.
func (a *Auth) IssueToken(username string) (string, error) {
	claims := jwt.MapClaims{
		"sub": username,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

// ValidateToken verifies a JWT and returns the username.
func (a *Auth) ValidateToken(tokenStr string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.jwtSecret, nil
	})
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", errors.New("invalid token")
	}

	sub, _ := claims.GetSubject()
	if sub == "" {
		return "", errors.New("missing subject")
	}
	return sub, nil
}

// Middleware returns an HTTP middleware that accepts either:
//   - X-API-Key header matching the configured apiKey (service-to-service), or
//   - Authorization: Bearer <JWT> (admin frontend).
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check API key first (service-to-service).
		if a.apiKey != "" && r.Header.Get("X-API-Key") == a.apiKey {
			ctx := context.WithValue(r.Context(), AuthTypeKey, AuthTypeAPIKey)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fall back to JWT Bearer token (admin frontend).
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		if _, err := a.ValidateToken(tokenStr); err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), AuthTypeKey, AuthTypeJWT)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GeneratePassword creates a random hex password of the given byte length.
func GeneratePassword(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
