package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var (
	ErrInvalidCredential = errors.New("invalid account or password")
	ErrAccountExists     = errors.New("account already exists")
	ErrAccountNotFound   = errors.New("account not found")
	ErrLastAccount       = errors.New("cannot delete the last admin account")
	ErrSessionNotFound   = errors.New("session not found or expired")
)

type Account struct {
	ID         int64
	Account    string
	CreateTime time.Time
}

// Store 只放後台自己的資料：管理員帳號與登入 session。商品、訂單都在 backend。
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS admin_account (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	account     TEXT    NOT NULL UNIQUE,
	pswd_hash   TEXT    NOT NULL,
	create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS admin_session (
	token       TEXT    PRIMARY KEY,
	account_id  INTEGER NOT NULL REFERENCES admin_account(id) ON DELETE CASCADE,
	expire_at   DATETIME NOT NULL,
	create_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_admin_session_expire ON admin_session(expire_at);
`)
	return err
}

func (s *Store) CountAccounts(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_account`).Scan(&n)
	return n, err
}

func (s *Store) CreateAccount(ctx context.Context, account, password string) (*Account, error) {
	if account == "" || password == "" {
		return nil, errors.New("account and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_account(account, pswd_hash) VALUES (?, ?)`, account, string(hash))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAccountExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.getAccountByID(ctx, id)
}

func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, account, create_time FROM admin_account ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Account, &a.CreateTime); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) error {
	n, err := s.CountAccounts(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAccount
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_account WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func (s *Store) UpdatePassword(ctx context.Context, id int64, oldPassword, newPassword string) error {
	if newPassword == "" {
		return errors.New("new password is required")
	}
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT pswd_hash FROM admin_account WHERE id = ?`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return ErrInvalidCredential
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE admin_account SET pswd_hash = ? WHERE id = ?`, string(newHash), id)
	return err
}

func (s *Store) Authenticate(ctx context.Context, account, password string) (*Account, error) {
	var a Account
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, account, pswd_hash, create_time FROM admin_account WHERE account = ?`, account).
		Scan(&a.ID, &a.Account, &hash, &a.CreateTime)
	if errors.Is(err, sql.ErrNoRows) {
		// 帳號不存在也跑一次 bcrypt，避免用回應時間判斷帳號是否存在。
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5XeGzQYfQ1FnIoQQVXghzTyvCB3G6"), []byte(password))
		return nil, ErrInvalidCredential
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidCredential
	}
	return &a, nil
}

func (s *Store) CreateSession(ctx context.Context, accountID int64, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_session(token, account_id, expire_at) VALUES (?, ?, ?)`,
		token, accountID, time.Now().Add(ttl).UTC())
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) GetSession(ctx context.Context, token string) (*Account, error) {
	var a Account
	var expireAt time.Time
	err := s.db.QueryRowContext(ctx, `
SELECT a.id, a.account, a.create_time, s.expire_at
FROM admin_session s JOIN admin_account a ON a.id = s.account_id
WHERE s.token = ?`, token).Scan(&a.ID, &a.Account, &a.CreateTime, &expireAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(expireAt) {
		_ = s.DeleteSession(ctx, token)
		return nil, ErrSessionNotFound
	}
	return &a, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_session WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_session WHERE expire_at < ?`, time.Now().UTC())
	return err
}

func (s *Store) getAccountByID(ctx context.Context, id int64) (*Account, error) {
	var a Account
	err := s.db.QueryRowContext(ctx,
		`SELECT id, account, create_time FROM admin_account WHERE id = ?`, id).
		Scan(&a.ID, &a.Account, &a.CreateTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	return &a, err
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
