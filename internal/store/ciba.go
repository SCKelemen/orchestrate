package store

import (
	"context"
	"database/sql"
	"fmt"
)

// CIBAState represents the state of a CIBA auth request.
type CIBAState string

const (
	CIBAPending  CIBAState = "PENDING"
	CIBAApproved CIBAState = "APPROVED"
	CIBADenied   CIBAState = "DENIED"
	CIBAExpired  CIBAState = "EXPIRED"
)

// CIBARequest represents a CIBA backchannel authentication request.
type CIBARequest struct {
	AuthReqID  string
	UserID     string
	ClientID   string
	Scope      string
	BindingMsg string
	State      CIBAState
	ExpiresAt  string
	Interval   int
	WebhookURL string
	CreateTime string
}

// CreateCIBARequestParams are the parameters for creating a CIBA request.
type CreateCIBARequestParams struct {
	AuthReqID  string
	UserID     string
	ClientID   string
	Scope      string
	BindingMsg string
	ExpiresAt  string
	Interval   int
	WebhookURL string
}

// CreateCIBARequest inserts a new CIBA auth request.
func (s *Store) CreateCIBARequest(ctx context.Context, p CreateCIBARequestParams) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ciba_requests (auth_req_id, user_id, client_id, scope, binding_message, expires_at, interval_s, webhook_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.AuthReqID, p.UserID, p.ClientID, p.Scope, p.BindingMsg, p.ExpiresAt, p.Interval, p.WebhookURL,
	)
	return err
}

// GetCIBARequest returns a CIBA request by auth_req_id.
func (s *Store) GetCIBARequest(ctx context.Context, authReqID string) (*CIBARequest, error) {
	var cr CIBARequest
	err := s.db.QueryRowContext(ctx,
		`SELECT auth_req_id, user_id, client_id, scope, binding_message, state, expires_at, interval_s, webhook_url, create_time
		 FROM ciba_requests WHERE auth_req_id = ?`, authReqID,
	).Scan(&cr.AuthReqID, &cr.UserID, &cr.ClientID, &cr.Scope, &cr.BindingMsg,
		&cr.State, &cr.ExpiresAt, &cr.Interval, &cr.WebhookURL, &cr.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ciba request: %w", err)
	}
	return &cr, nil
}

// ApproveCIBARequest marks a CIBA request as approved.
func (s *Store) ApproveCIBARequest(ctx context.Context, authReqID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE ciba_requests SET state = 'APPROVED' WHERE auth_req_id = ? AND state = 'PENDING'`,
		authReqID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("ciba request not found or not pending")
	}
	return nil
}

// DenyCIBARequest marks a CIBA request as denied.
func (s *Store) DenyCIBARequest(ctx context.Context, authReqID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ciba_requests SET state = 'DENIED' WHERE auth_req_id = ? AND state = 'PENDING'`,
		authReqID,
	)
	return err
}
