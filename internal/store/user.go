package store

import (
	"context"
	"database/sql"
	"fmt"
)

// User represents an authenticated user.
type User struct {
	ID          string
	DisplayName string
	Email       string
	State       string
	CreateTime  string
	UpdateTime  string
}

// CreateUserParams are the parameters for creating a user.
type CreateUserParams struct {
	DisplayName string
	Email       string
}

// CreateUser inserts a new user.
func (s *Store) CreateUser(ctx context.Context, id string, p CreateUserParams) (*User, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, display_name, email) VALUES (?, ?, ?)`,
		id, p.DisplayName, p.Email,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return s.GetUser(ctx, id)
}

// GetUser returns a user by ID.
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, display_name, email, state, create_time, update_time FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.DisplayName, &u.Email, &u.State, &u.CreateTime, &u.UpdateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

// GetUserByEmail returns a user by email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`SELECT id, display_name, email, state, create_time, update_time FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.DisplayName, &u.Email, &u.State, &u.CreateTime, &u.UpdateTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &u, nil
}

// UpdateUser updates user fields. Only non-nil fields are updated.
func (s *Store) UpdateUser(ctx context.Context, id string, displayName, email *string) error {
	if displayName != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET display_name = ?, update_time = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
			*displayName, id,
		); err != nil {
			return fmt.Errorf("update user display_name: %w", err)
		}
	}
	if email != nil {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users SET email = ?, update_time = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
			*email, id,
		); err != nil {
			return fmt.Errorf("update user email: %w", err)
		}
	}
	return nil
}

// DeleteUser removes a user.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found: %s", id)
	}
	return nil
}

// ListUsersParams are the parameters for listing users.
type ListUsersParams struct {
	PageSize  int
	PageToken string
}

// ListUsers returns a page of users.
func (s *Store) ListUsers(ctx context.Context, p ListUsersParams) ([]*User, error) {
	if p.PageSize <= 0 {
		p.PageSize = 20
	}
	limit := p.PageSize + 1

	var rows *sql.Rows
	var err error
	if p.PageToken != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, display_name, email, state, create_time, update_time
			 FROM users WHERE id > ? ORDER BY id LIMIT ?`,
			p.PageToken, limit,
		)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, display_name, email, state, create_time, update_time
			 FROM users ORDER BY id LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email, &u.State, &u.CreateTime, &u.UpdateTime); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}
