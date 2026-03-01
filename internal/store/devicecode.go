package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// DeviceCodeState represents the state of a device code.
type DeviceCodeState string

const (
	DeviceCodePending  DeviceCodeState = "PENDING"
	DeviceCodeApproved DeviceCodeState = "APPROVED"
	DeviceCodeDenied   DeviceCodeState = "DENIED"
	DeviceCodeExpired  DeviceCodeState = "EXPIRED"
	DeviceCodeConsumed DeviceCodeState = "CONSUMED"
)

var ErrDeviceCodeNotConsumable = errors.New("device code not approved or already consumed")

// DeviceCode represents a device authorization code.
type DeviceCode struct {
	DeviceCode string
	UserCode   string
	ClientID   string
	Scope      string
	UserID     *string
	State      DeviceCodeState
	ExpiresAt  string
	Interval   int
	CreateTime string
}

// CreateDeviceCodeParams are the parameters for creating a device code.
type CreateDeviceCodeParams struct {
	DeviceCode string
	UserCode   string
	ClientID   string
	Scope      string
	ExpiresAt  string
	Interval   int
}

// CreateDeviceCode inserts a new device code.
func (s *Store) CreateDeviceCode(ctx context.Context, p CreateDeviceCodeParams) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO device_codes (device_code, user_code, client_id, scope, expires_at, interval_s)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.DeviceCode, p.UserCode, p.ClientID, p.Scope, p.ExpiresAt, p.Interval,
	)
	return err
}

// GetDeviceCode returns a device code by device_code value.
func (s *Store) GetDeviceCode(ctx context.Context, deviceCode string) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.db.QueryRowContext(ctx,
		`SELECT device_code, user_code, client_id, scope, user_id, state, expires_at, interval_s, create_time
		 FROM device_codes WHERE device_code = ?`, deviceCode,
	).Scan(&dc.DeviceCode, &dc.UserCode, &dc.ClientID, &dc.Scope, &dc.UserID,
		&dc.State, &dc.ExpiresAt, &dc.Interval, &dc.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get device code: %w", err)
	}
	return &dc, nil
}

// GetDeviceCodeByUserCode returns a device code by user_code.
func (s *Store) GetDeviceCodeByUserCode(ctx context.Context, userCode string) (*DeviceCode, error) {
	var dc DeviceCode
	err := s.db.QueryRowContext(ctx,
		`SELECT device_code, user_code, client_id, scope, user_id, state, expires_at, interval_s, create_time
		 FROM device_codes WHERE user_code = ?`, userCode,
	).Scan(&dc.DeviceCode, &dc.UserCode, &dc.ClientID, &dc.Scope, &dc.UserID,
		&dc.State, &dc.ExpiresAt, &dc.Interval, &dc.CreateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get device code by user code: %w", err)
	}
	return &dc, nil
}

// ApproveDeviceCode marks a device code as approved with the given user.
func (s *Store) ApproveDeviceCode(ctx context.Context, deviceCode, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE device_codes SET state = 'APPROVED', user_id = ? WHERE device_code = ? AND state = 'PENDING'`,
		userID, deviceCode,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("device code not found or not pending")
	}
	return nil
}

// DenyDeviceCode marks a device code as denied.
func (s *Store) DenyDeviceCode(ctx context.Context, deviceCode string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE device_codes SET state = 'DENIED' WHERE device_code = ? AND state = 'PENDING'`,
		deviceCode,
	)
	return err
}

// ConsumeDeviceCode atomically marks an approved device code as consumed.
func (s *Store) ConsumeDeviceCode(ctx context.Context, deviceCode string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE device_codes SET state = 'CONSUMED' WHERE device_code = ? AND state = 'APPROVED'`,
		deviceCode,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDeviceCodeNotConsumable
	}
	return nil
}
