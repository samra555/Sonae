package database_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"gotovnost/internal/database"
)

func TestRegisterAndAuthenticate(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	if err := accounts.Register("kirill", "password123"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !accounts.Authenticate("kirill", "password123") {
		t.Fatal("correct password should authenticate")
	}
	if accounts.Authenticate("kirill", "wrong") {
		t.Fatal("wrong password should not authenticate")
	}
	if !accounts.Authenticate("KIRILL", "password123") {
		t.Fatal("login should be normalized to lower case, so KIRILL authenticates")
	}
}

func TestRegisterRejectsInvalidLogin(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	for _, login := range []string{"", "a", "кирилл", "with space"} {
		if err := accounts.Register(login, "password123"); err == nil {
			t.Errorf("expected error for login %q", login)
		}
	}
}

func TestRegisterNormalizesLogin(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	if err := accounts.Register("KIRILL", "password123"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !accounts.Authenticate("kirill", "password123") {
		t.Fatal("registered login should be stored lower-cased")
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	if err := accounts.Register("kirill", "short"); err == nil {
		t.Fatal("expected short password error")
	}
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	if err := accounts.Register("kirill", "password123"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := accounts.Register("kirill", "password456"); err == nil {
		t.Fatal("expected duplicate login error")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	value, _ := accounts.NewSessionValue("kirill")
	if got := accounts.LoginFromSession(value); got != "kirill" {
		t.Fatalf("LoginFromSession = %q, want kirill", got)
	}
}

func TestSessionRejectsTampered(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	value, _ := accounts.NewSessionValue("kirill")
	parts := strings.Split(value, "|")
	tampered := parts[0] + "|" + parts[1] + "|" + "AAAA"
	if got := accounts.LoginFromSession(tampered); got != "" {
		t.Fatalf("tampered session = %q, want empty", got)
	}
}

func TestSessionRejectsMalformed(t *testing.T) {
	accounts, err := database.OpenAccounts(t.TempDir())
	if err != nil {
		t.Fatalf("OpenAccounts: %v", err)
	}
	for _, value := range []string{"", "a|b", "a|b|c|d"} {
		if got := accounts.LoginFromSession(value); got != "" {
			t.Errorf("malformed session %q = %q, want empty", value, got)
		}
	}
}

func TestPBKDF2KnownVector(t *testing.T) {
	// RFC 8018 PBKDF2-HMAC-SHA256, c=1.
	got := database.PBKDF2SHA256([]byte("password"), []byte("salt"), 1)
	want := "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
	if hex.EncodeToString(got) != want {
		t.Fatalf("pbkdf2 = %x, want %s", got, want)
	}
}

func TestPBKDF2Deterministic(t *testing.T) {
	a := database.PBKDF2SHA256([]byte("password"), []byte("salt"), 1000)
	b := database.PBKDF2SHA256([]byte("password"), []byte("salt"), 1000)
	if hex.EncodeToString(a) != hex.EncodeToString(b) {
		t.Fatal("same input should give same output")
	}
}
