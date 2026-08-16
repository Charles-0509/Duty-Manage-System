package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	refreshTokenLifetime = 7 * 24 * time.Hour
	refreshTokenBytes    = 32
)

var ErrRefreshTokenInvalid = errors.New("登录状态已失效，请重新登录")

// IssueRefreshToken creates a new rotating refresh token for the account and
// returns the raw token. Only the SHA-256 hash of the raw token is persisted.
func (s *Store) IssueRefreshToken(accountID int64) (string, error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	now := time.Now()
	_, err := s.control.Exec(`
		INSERT INTO refresh_tokens (account_id, token_hash, created_at, expires_at)
		VALUES (?, ?, ?, ?)
	`, accountID, hashRefreshToken(token), now.Format(time.DateTime), now.Add(refreshTokenLifetime).Format(time.DateTime))
	if err != nil {
		return "", err
	}
	s.pruneRefreshTokens(accountID)
	return token, nil
}

// RotateRefreshToken validates a refresh token, revokes it, and issues a new
// one for the same account. Reuse of an already-rotated token is rejected.
func (s *Store) RotateRefreshToken(rawToken string) (int64, string, error) {
	if rawToken == "" {
		return 0, "", ErrRefreshTokenInvalid
	}
	tokenHash := hashRefreshToken(rawToken)

	tx, err := s.control.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	var id int64
	var accountID int64
	var expiresAt string
	var revokedAt sql.NullString
	err = tx.QueryRow(`
		SELECT id, account_id, expires_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = ?
	`, tokenHash).Scan(&id, &accountID, &expiresAt, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrRefreshTokenInvalid
		}
		return 0, "", err
	}
	if revokedAt.Valid {
		return 0, "", ErrRefreshTokenInvalid
	}
	expiry, err := time.Parse(time.DateTime, expiresAt)
	if err != nil || expiry.Before(time.Now()) {
		return 0, "", ErrRefreshTokenInvalid
	}

	if _, err := tx.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE id = ?`, time.Now().Format(time.DateTime), id); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}

	next, err := s.IssueRefreshToken(accountID)
	if err != nil {
		return 0, "", err
	}
	return accountID, next, nil
}

func (s *Store) RevokeRefreshToken(rawToken string) {
	if rawToken == "" {
		return
	}
	_, _ = s.control.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, time.Now().Format(time.DateTime), hashRefreshToken(rawToken))
}

func (s *Store) RevokeAccountRefreshTokens(accountID int64) {
	_, _ = s.control.Exec(`UPDATE refresh_tokens SET revoked_at = ? WHERE account_id = ? AND revoked_at IS NULL`, time.Now().Format(time.DateTime), accountID)
}

// BumpSessionVersion invalidates every outstanding access token for the
// account. Called on password change and admin password reset.
func (s *Store) BumpSessionVersion(accountID int64) error {
	_, err := s.control.Exec(`UPDATE accounts SET session_version = session_version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, accountID)
	return err
}

func (s *Store) pruneRefreshTokens(accountID int64) {
	_, _ = s.control.Exec(`DELETE FROM refresh_tokens WHERE account_id = ? AND (expires_at < ? OR revoked_at IS NOT NULL)`, accountID, time.Now().Add(-24*time.Hour).Format(time.DateTime))
}

func hashRefreshToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func (s *Store) sessionVersionForAccount(accountID int64) (int64, error) {
	var version int64
	if err := s.control.QueryRow(`SELECT session_version FROM accounts WHERE id = ?`, accountID).Scan(&version); err != nil {
		return 0, fmt.Errorf("账户不存在")
	}
	return version, nil
}
