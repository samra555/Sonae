package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Layout of the data directory:
//
//	users.json           logins and password hashes
//	session.key          secret that signs session cookies
//	accounts/<login>.json  cases and profile of one account (the same format the old data.json had)
const (
	usersFileName      = "users.json"
	sessionKeyFileName = "session.key"
	accountsDirName    = "accounts"

	minPasswordLength = 8
	// OWASP recommendation for PBKDF2-HMAC-SHA256.
	passwordIterations = 600_000
	sessionLifetime    = 30 * 24 * time.Hour
)

// The login becomes a file name, so it is restricted to safe characters.
var loginPattern = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)

type User struct {
	Login        string `json:"login"`
	Salt         string `json:"salt"`
	PasswordHash string `json:"password_hash"`
	Iterations   int    `json:"iterations"`
	CreatedAt    string `json:"created_at"`
}

type usersFile struct {
	Users []User `json:"users"`
}

type Accounts struct {
	dir string
	// legacyDataPath is the old single-user data.json; the first account created takes over its data.
	legacyDataPath string
	sessionKey     []byte

	mu     sync.Mutex
	users  []User
	stores map[string]*Store
}

func OpenAccounts(dir, legacyDataPath string) (*Accounts, error) {
	if err := os.MkdirAll(filepath.Join(dir, accountsDirName), 0o755); err != nil {
		return nil, err
	}
	accounts := &Accounts{dir: dir, legacyDataPath: legacyDataPath, stores: map[string]*Store{}}

	rawUsers, err := os.ReadFile(filepath.Join(dir, usersFileName))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		var file usersFile
		if err := json.Unmarshal(rawUsers, &file); err != nil {
			return nil, fmt.Errorf("%s: неверный JSON: %w", usersFileName, err)
		}
		accounts.users = file.Users
	}

	if accounts.sessionKey, err = loadOrCreateSessionKey(filepath.Join(dir, sessionKeyFileName)); err != nil {
		return nil, err
	}
	return accounts, nil
}

func loadOrCreateSessionKey(path string) ([]byte, error) {
	rawKey, err := os.ReadFile(path)
	if err == nil {
		return hex.DecodeString(strings.TrimSpace(string(rawKey)))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600)
}

func (a *Accounts) HasUsers() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.users) > 0
}

func (a *Accounts) findUser(login string) *User {
	for userIndex := range a.users {
		if a.users[userIndex].Login == login {
			return &a.users[userIndex]
		}
	}
	return nil
}

func (a *Accounts) dataPath(login string) string {
	return filepath.Join(a.dir, accountsDirName, login+".json")
}

func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

func (a *Accounts) Register(login, password string) error {
	login = normalizeLogin(login)
	if !loginPattern.MatchString(login) {
		return errors.New("логин: 2–32 символа, латиница в нижнем регистре, цифры, «-» и «_»")
	}
	if len([]rune(password)) < minPasswordLength {
		return fmt.Errorf("пароль: не короче %d символов", minPasswordLength)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.findUser(login) != nil {
		return errors.New("такой логин уже занят")
	}

	if err := a.createAccountData(login, len(a.users) == 0); err != nil {
		return err
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	newUser := User{
		Login:        login,
		Salt:         hex.EncodeToString(salt),
		PasswordHash: hex.EncodeToString(pbkdf2SHA256([]byte(password), salt, passwordIterations)),
		Iterations:   passwordIterations,
		CreatedAt:    time.Now().Format("2006-01-02"),
	}
	updatedUsers := append(append([]User{}, a.users...), newUser)
	if err := writeJSONAtomically(filepath.Join(a.dir, usersFileName), usersFile{Users: updatedUsers}); err != nil {
		return err
	}
	a.users = updatedUsers
	return nil
}

// createAccountData writes the new account's data file: the first account inherits the old
// single-user data.json if there is one, every other account starts empty.
func (a *Accounts) createAccountData(login string, isFirstAccount bool) error {
	path := a.dataPath(login)
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	if isFirstAccount && a.legacyDataPath != "" {
		legacyBytes, err := os.ReadFile(a.legacyDataPath)
		if err == nil {
			return os.WriteFile(path, legacyBytes, 0o644)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return writeJSONAtomically(path, Data{Cases: []Case{}})
}

func (a *Accounts) Authenticate(login, password string) bool {
	login = normalizeLogin(login)

	a.mu.Lock()
	user := a.findUser(login)
	var storedUser User
	if user != nil {
		storedUser = *user
	}
	a.mu.Unlock()

	if user == nil {
		return false
	}
	salt, saltErr := hex.DecodeString(storedUser.Salt)
	expectedHash, hashErr := hex.DecodeString(storedUser.PasswordHash)
	if saltErr != nil || hashErr != nil {
		return false
	}
	return hmac.Equal(pbkdf2SHA256([]byte(password), salt, storedUser.Iterations), expectedHash)
}

// StoreFor opens the account's data on first use and keeps it open afterwards.
func (a *Accounts) StoreFor(login string) (*Store, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.findUser(login) == nil {
		return nil, errors.New("учётка не найдена")
	}
	if store, ok := a.stores[login]; ok {
		return store, nil
	}
	store, err := NewStore(a.dataPath(login))
	if err != nil {
		return nil, err
	}
	a.stores[login] = store
	return store, nil
}

// ---- sessions ----

// A session cookie is "login|expiry|signature". It is signed rather than stored server-side,
// so sessions survive a server restart.
func (a *Accounts) sessionSignature(payload string) string {
	mac := hmac.New(sha256.New, a.sessionKey)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *Accounts) NewSessionValue(login string) (string, time.Time) {
	expiresAt := time.Now().Add(sessionLifetime)
	payload := normalizeLogin(login) + "|" + strconv.FormatInt(expiresAt.Unix(), 10)
	return payload + "|" + a.sessionSignature(payload), expiresAt
}

// LoginFromSession returns the login of a valid, unexpired session value, or "".
func (a *Accounts) LoginFromSession(sessionValue string) string {
	parts := strings.Split(sessionValue, "|")
	if len(parts) != 3 {
		return ""
	}
	payload := parts[0] + "|" + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(a.sessionSignature(payload))) {
		return ""
	}
	expiresAtUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > expiresAtUnix {
		return ""
	}
	return parts[0]
}

// pbkdf2SHA256 is PBKDF2 (RFC 8018) with HMAC-SHA256 and a 32-byte key. go.mod targets Go 1.22,
// which has no crypto/pbkdf2 yet, and the project keeps zero external dependencies.
func pbkdf2SHA256(password, salt []byte, iterations int) []byte {
	mac := hmac.New(sha256.New, password)

	blockIndex := make([]byte, 4)
	binary.BigEndian.PutUint32(blockIndex, 1)
	mac.Write(salt)
	mac.Write(blockIndex)
	previous := mac.Sum(nil)

	derived := append([]byte{}, previous...)
	for iteration := 1; iteration < iterations; iteration++ {
		mac.Reset()
		mac.Write(previous)
		previous = mac.Sum(previous[:0])
		for byteIndex := range derived {
			derived[byteIndex] ^= previous[byteIndex]
		}
	}
	return derived
}
