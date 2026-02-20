package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Credential represents a user authentication credential.
type Credential struct {
	ID             string
	UserID         string
	CredentialType string // "bearer", "github", "google", "webauthn"
	ExternalID     string
	PublicKey      []byte
	Metadata       string
	CreateTime     string
}

// CreateCredentialParams are the parameters for creating a credential.
type CreateCredentialParams struct {
	UserID         string
	CredentialType string
	ExternalID     string
	PublicKey      []byte
	Metadata       string
}

// CreateCredential inserts a new credential.
func (s *Store) CreateCredential(ctx context.Context, id string, p CreateCredentialParams) (*Credential, error) {
	metadata := p.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO credentials (id, user_id, credential_type, external_id, public_key, metadata)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, p.UserID, p.CredentialType, p.ExternalID, p.PublicKey, metadata,
	)
	if err != nil {
		return nil, fmt.Errorf("insert credential: %w", err)
	}
	return s.GetCredential(ctx, id)
}

// GetCredential returns a credential by ID.
func (s *Store) GetCredential(ctx context.Context, id string) (*Credential, error) {
	var c Credential
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, credential_type, external_id, public_key, metadata, create_time
		 FROM credentials WHERE id = ?`, id,
	).Scan(&c.ID, &c.UserID, &c.CredentialType, &c.ExternalID, &c.PublicKey, &c.Metadata, &c.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}
	return &c, nil
}

// GetCredentialByExternal returns a credential by type and external ID.
func (s *Store) GetCredentialByExternal(ctx context.Context, credType, externalID string) (*Credential, error) {
	var c Credential
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, credential_type, external_id, public_key, metadata, create_time
		 FROM credentials WHERE credential_type = ? AND external_id = ?`,
		credType, externalID,
	).Scan(&c.ID, &c.UserID, &c.CredentialType, &c.ExternalID, &c.PublicKey, &c.Metadata, &c.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get credential by external: %w", err)
	}
	return &c, nil
}

// ListCredentials returns all credentials for a user.
func (s *Store) ListCredentials(ctx context.Context, userID string) ([]*Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, credential_type, external_id, public_key, metadata, create_time
		 FROM credentials WHERE user_id = ? ORDER BY create_time`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()

	var creds []*Credential
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.UserID, &c.CredentialType, &c.ExternalID, &c.PublicKey, &c.Metadata, &c.CreateTime); err != nil {
			return nil, fmt.Errorf("scan credential: %w", err)
		}
		creds = append(creds, &c)
	}
	return creds, rows.Err()
}

// DeleteCredential removes a credential.
func (s *Store) DeleteCredential(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("credential not found: %s", id)
	}
	return nil
}
