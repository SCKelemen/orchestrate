package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// Session represents an active refresh token session.
type Session struct {
	ID         string
	UserID     string
	TokenHash  string
	Provider   string
	IPAddress  string
	UserAgent  string
	ExpiresAt  string
	CreateTime string
	RevokedAt  *string
}

// CreateSessionParams are the parameters for creating a session.
type CreateSessionParams struct {
	UserID       string
	RefreshToken string // plaintext; will be hashed before storage
	Provider     string
	IPAddress    string
	UserAgent    string
	ExpiresAt    string
}

// HashToken returns the SHA256 hex hash of a token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// CreateSession inserts a new session with the refresh token hashed.
func (s *Store) CreateSession(ctx context.Context, id string, p CreateSessionParams) (*Session, error) {
	tokenHash := HashToken(p.RefreshToken)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, provider, ip_address, user_agent, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, p.UserID, tokenHash, p.Provider, p.IPAddress, p.UserAgent, p.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return s.getSession(ctx, id)
}

func (s *Store) getSession(ctx context.Context, id string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, provider, ip_address, user_agent, expires_at, create_time, revoked_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.Provider, &sess.IPAddress,
		&sess.UserAgent, &sess.ExpiresAt, &sess.CreateTime, &sess.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

// GetSessionByTokenHash returns an active (non-revoked, non-expired) session matching the hash.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, provider, ip_address, user_agent, expires_at, create_time, revoked_at
		 FROM sessions
		 WHERE token_hash = ?
		   AND revoked_at IS NULL
		   AND expires_at > strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		tokenHash,
	).Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.Provider, &sess.IPAddress,
		&sess.UserAgent, &sess.ExpiresAt, &sess.CreateTime, &sess.RevokedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session by token hash: %w", err)
	}
	return &sess, nil
}

// RevokeSession marks a session as revoked.
func (s *Store) RevokeSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`, id,
	)
	return err
}

// RevokeAllSessions revokes all sessions for a user.
func (s *Store) RevokeAllSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		 WHERE user_id = ? AND revoked_at IS NULL`, userID,
	)
	return err
}

// CleanExpiredSessions removes sessions that expired before now.
func (s *Store) CleanExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
