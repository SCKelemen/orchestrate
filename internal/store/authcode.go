package store

import (
	"context"
	"database/sql"
	"fmt"
)

// AuthCode represents an authorization code for the auth code + PKCE flow.
type AuthCode struct {
	Code                string
	UserID              string
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           string
	Consumed            bool
	CreateTime          string
}

// CreateAuthCodeParams are the parameters for creating an auth code.
type CreateAuthCodeParams struct {
	UserID              string
	ClientID            string
	RedirectURI         string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           string
}

// CreateAuthCode inserts a new authorization code.
func (s *Store) CreateAuthCode(ctx context.Context, code string, p CreateAuthCodeParams) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO auth_codes (code, user_id, client_id, redirect_uri, scope, code_challenge, code_challenge_method, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		code, p.UserID, p.ClientID, p.RedirectURI, p.Scope, p.CodeChallenge, p.CodeChallengeMethod, p.ExpiresAt,
	)
	return err
}

// GetAuthCode returns an auth code if it exists, is not expired, and not consumed.
func (s *Store) GetAuthCode(ctx context.Context, code string) (*AuthCode, error) {
	var ac AuthCode
	var consumed int
	err := s.db.QueryRowContext(ctx,
		`SELECT code, user_id, client_id, redirect_uri, scope, code_challenge, code_challenge_method, expires_at, consumed, create_time
		 FROM auth_codes WHERE code = ?`, code,
	).Scan(&ac.Code, &ac.UserID, &ac.ClientID, &ac.RedirectURI, &ac.Scope,
		&ac.CodeChallenge, &ac.CodeChallengeMethod, &ac.ExpiresAt, &consumed, &ac.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get auth code: %w", err)
	}
	ac.Consumed = consumed != 0
	return &ac, nil
}

// ConsumeAuthCode marks an auth code as consumed (one-time use).
func (s *Store) ConsumeAuthCode(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_codes SET consumed = 1 WHERE code = ? AND consumed = 0`, code,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("auth code not found or already consumed")
	}
	return nil
}
