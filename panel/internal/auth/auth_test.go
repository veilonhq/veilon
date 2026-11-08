package auth

import (
	"testing"
	"time"

	"github.com/veilonhq/veilon/panel/internal/db"
	"github.com/veilonhq/veilon/panel/internal/testutil"
)

func TestHashAndCheck(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") || CheckPassword(h, "wrong") {
		t.Fatal("bcrypt check mismatch")
	}
	if ValidatePassword("short") == nil {
		t.Fatal("short password accepted")
	}
}

func TestJWTIssueVerify(t *testing.T) {
	gdb := testutil.DB(t)
	a, err := CreateAdmin(gdb, "root", "password123", true)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServiceWithSecret(gdb, []byte("secret-a"), time.Hour)
	tok, exp, err := s.Issue(a)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) < 59*time.Minute {
		t.Fatalf("bad expiry %v", exp)
	}
	got, err := s.Verify(tok)
	if err != nil || got.ID != a.ID || !got.IsSudo {
		t.Fatalf("verify: %v %+v", err, got)
	}
	other := NewServiceWithSecret(gdb, []byte("secret-b"), time.Hour)
	if _, err := other.Verify(tok); err == nil {
		t.Fatal("token verified with wrong secret")
	}
	if _, err := s.Verify(tok + "x"); err == nil {
		t.Fatal("tampered token accepted")
	}
	expired := NewServiceWithSecret(gdb, []byte("secret-a"), -time.Minute)
	etok, _, _ := expired.Issue(a)
	if _, err := s.Verify(etok); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestPasswordChangeInvalidatesTokens(t *testing.T) {
	gdb := testutil.DB(t)
	a, _ := CreateAdmin(gdb, "root", "password123", true)
	s := NewServiceWithSecret(gdb, []byte("k"), time.Hour)
	tok, _, _ := s.Issue(a)
	time.Sleep(1100 * time.Millisecond)
	if err := SetPassword(gdb, a, "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(tok); err == nil {
		t.Fatal("old token still valid after password change")
	}
	var fresh db.Admin
	gdb.First(&fresh, a.ID)
	if !CheckPassword(fresh.PasswordHash, "newpassword1") {
		t.Fatal("password not updated")
	}
	tok2, _, _ := s.Issue(&fresh)
	if _, err := s.Verify(tok2); err != nil {
		t.Fatalf("new token rejected: %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	gdb := testutil.DB(t)
	if _, err := CreateAdmin(gdb, "root", "password123", true); err != nil {
		t.Fatal(err)
	}
	s := NewServiceWithSecret(gdb, []byte("k"), time.Hour)
	for i := 0; i < 5; i++ {
		if _, _, _, err := s.Login("1.2.3.4", "root", "bad"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, _, err := s.Login("1.2.3.4", "root", "password123"); err != ErrRateLimited {
		t.Fatalf("expected rate limit, got %v", err)
	}
	// Other IPs are unaffected.
	if _, tok, _, err := s.Login("5.6.7.8", "root", "password123"); err != nil || tok == "" {
		t.Fatalf("other ip login failed: %v", err)
	}
}

func TestLimiterWindow(t *testing.T) {
	l := NewLimiter(2, 10*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.Fail("a")
	l.Fail("a")
	if l.Allow("a") {
		t.Fatal("should be limited")
	}
	now = now.Add(11 * time.Minute)
	if !l.Allow("a") {
		t.Fatal("window should have expired")
	}
}

func TestCreateAdminValidation(t *testing.T) {
	gdb := testutil.DB(t)
	if _, err := CreateAdmin(gdb, "a", "password123", false); err == nil {
		t.Fatal("short username accepted")
	}
	if _, err := CreateAdmin(gdb, "root", "password123", false); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateAdmin(gdb, "root", "password123", false); err == nil {
		t.Fatal("duplicate admin accepted")
	}
}
