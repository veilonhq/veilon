// Package auth implements admin password hashing, JWT issuing/verification
// and per-IP login rate limiting.
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/util"
)

// Errors returned by the auth service.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrRateLimited        = errors.New("too many failed login attempts")
	ErrAdminDisabled      = errors.New("this admin account is disabled")
)

// HashPassword returns a bcrypt hash.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword compares a bcrypt hash and a password.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ValidatePassword enforces a minimal password policy.
func ValidatePassword(pw string) error {
	if len(pw) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(pw) > 72 {
		return errors.New("password must be at most 72 bytes")
	}
	return nil
}

var (
	dummyOnce sync.Once
	dummy     []byte
)

func dummyHash() []byte {
	dummyOnce.Do(func() { dummy, _ = bcrypt.GenerateFromPassword([]byte(util.RandHex(8)), bcrypt.DefaultCost) })
	return dummy
}

// Service issues and verifies tokens.
type Service struct {
	DB      *gorm.DB
	TTL     time.Duration
	secret  []byte
	Limiter *Limiter
}

// NewService loads (or generates at first run) the JWT secret from settings.
func NewService(gdb *gorm.DB, ttl time.Duration) (*Service, error) {
	sec, err := db.SetDefault(gdb, db.KeyJWTSecret, util.RandHex(32))
	if err != nil {
		return nil, err
	}
	return &Service{DB: gdb, TTL: ttl, secret: []byte(sec), Limiter: NewLimiter(5, 10*time.Minute)}, nil
}

// NewServiceWithSecret is used by tests.
func NewServiceWithSecret(gdb *gorm.DB, secret []byte, ttl time.Duration) *Service {
	return &Service{DB: gdb, TTL: ttl, secret: secret, Limiter: NewLimiter(5, 10*time.Minute)}
}

// Claims is the JWT payload.
type Claims struct {
	Username string `json:"username"`
	Sudo     bool   `json:"sudo"`
	jwt.RegisteredClaims
}

// Issue creates a signed token for an admin.
func (s *Service) Issue(a *db.Admin) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(s.TTL)
	c := Claims{
		Username: a.Username,
		Sudo:     a.IsSudo,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(a.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    "veilon",
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
	return tok, exp, err
}

// Verify parses a token and loads the admin. Tokens issued before the
// admin's last password change are rejected.
func (s *Service) Verify(token string) (*db.Admin, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected alg %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("veilon"))
	if err != nil {
		return nil, ErrInvalidToken
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return nil, ErrInvalidToken
	}
	var a db.Admin
	if err := s.DB.First(&a, id).Error; err != nil {
		return nil, ErrInvalidToken
	}
	if a.Disabled {
		return nil, ErrInvalidToken
	}
	if a.PasswordChangedAt != nil && c.IssuedAt != nil && c.IssuedAt.Time.Before(a.PasswordChangedAt.Truncate(time.Second)) {
		return nil, ErrInvalidToken
	}
	return &a, nil
}

// Login checks credentials with rate limiting by ip.
func (s *Service) Login(ip, username, password string) (*db.Admin, string, time.Time, error) {
	if !s.Limiter.Allow(ip) {
		return nil, "", time.Time{}, ErrRateLimited
	}
	var a db.Admin
	err := s.DB.Where("username = ?", username).First(&a).Error
	if err != nil {
		// Spend comparable time to avoid user enumeration by timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		s.Limiter.Fail(ip)
		return nil, "", time.Time{}, ErrInvalidCredentials
	}
	if !CheckPassword(a.PasswordHash, password) {
		s.Limiter.Fail(ip)
		return nil, "", time.Time{}, ErrInvalidCredentials
	}
	s.Limiter.Reset(ip)
	if a.Disabled {
		return nil, "", time.Time{}, ErrAdminDisabled
	}
	tok, exp, err := s.Issue(&a)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return &a, tok, exp, nil
}

// SetPassword updates an admin's password and invalidates older tokens.
func SetPassword(gdb *gorm.DB, a *db.Admin, newPassword string) error {
	h, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	a.PasswordHash = h
	a.PasswordChangedAt = &now
	return gdb.Model(a).Updates(map[string]any{"password_hash": h, "password_changed_at": now, "updated_at": now}).Error
}

// CreateAdmin inserts a new admin.
func CreateAdmin(gdb *gorm.DB, username, password string, sudo bool) (*db.Admin, error) {
	if !validAdminName(username) {
		return nil, ErrBadUsername
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}
	var n int64
	gdb.Model(&db.Admin{}).Where("username = ?", username).Count(&n)
	if n > 0 {
		return nil, errors.New("admin already exists")
	}
	h, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	a := &db.Admin{Username: username, PasswordHash: h, IsSudo: sudo}
	if err := gdb.Create(a).Error; err != nil {
		return nil, err
	}
	return a, nil
}

// Errors returned by RenameAdmin.
var (
	ErrBadUsername   = errors.New("username must be 3-32 chars of letters, digits, _ . -")
	ErrUsernameTaken = errors.New("username is already taken")
)

// RenameAdmin changes an admin's username after validating format and
// uniqueness. Tokens stay valid (they are bound to the admin id).
func RenameAdmin(gdb *gorm.DB, a *db.Admin, username string) error {
	if !validAdminName(username) {
		return ErrBadUsername
	}
	if username == a.Username {
		return nil
	}
	var n int64
	if err := gdb.Model(&db.Admin{}).Where("username = ? AND id <> ?", username, a.ID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return ErrUsernameTaken
	}
	now := time.Now().UTC()
	if err := gdb.Model(a).Updates(map[string]any{"username": username, "updated_at": now}).Error; err != nil {
		return err
	}
	a.Username, a.UpdatedAt = username, now
	return nil
}

func validAdminName(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// Limiter counts failed attempts per key inside a sliding window.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
	now    func() time.Time
}

// NewLimiter returns a limiter allowing max failures per window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, fails: map[string][]time.Time{}, now: time.Now}
}

func (l *Limiter) prune(key string) []time.Time {
	cut := l.now().Add(-l.window)
	ts := l.fails[key]
	i := 0
	for ; i < len(ts) && ts[i].Before(cut); i++ {
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(l.fails, key)
	} else {
		l.fails[key] = ts
	}
	return ts
}

// Allow reports whether another attempt is permitted.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key)) < l.max
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(key)
	l.fails[key] = append(l.fails[key], l.now())
}

// Reset clears failures for key.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// Errors of DeleteAdmin.
var (
	ErrAdminNotFound = errors.New("admin not found")
	ErrLastSudo      = errors.New("cannot delete the last full (sudo) admin")
)

// AdminOwnsUsersError refuses deleting an admin who still owns users.
type AdminOwnsUsersError struct{ N int64 }

func (e *AdminOwnsUsersError) Error() string {
	return fmt.Sprintf("this admin owns %d users; move or delete them first", e.N)
}

// DeleteAdmin removes an admin by username. It refuses the last full
// (sudo) admin and an admin who owns users (their users would be left
// without an owner). The checks and the delete run in one transaction.
func DeleteAdmin(gdb *gorm.DB, username string) (*db.Admin, error) {
	var a db.Admin
	err := gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("username = ?", username).Limit(1).Find(&a).Error; err != nil {
			return err
		}
		if a.ID == 0 {
			return ErrAdminNotFound
		}
		if a.IsSudo {
			var n int64
			tx.Model(&db.Admin{}).Where("is_sudo = ? AND disabled = ? AND id <> ?", true, false, a.ID).Count(&n)
			if n == 0 {
				return ErrLastSudo
			}
		}
		var owned int64
		tx.Model(&db.User{}).Where("admin_id = ?", a.ID).Count(&owned)
		if owned > 0 {
			return &AdminOwnsUsersError{N: owned}
		}
		return tx.Delete(&db.Admin{}, a.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &a, nil
}
