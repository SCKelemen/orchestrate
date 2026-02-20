package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database for task and run persistence.
type Store struct {
	db *sql.DB
}

// Open creates or opens a SQLite database at the given path and runs migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}


func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
	id          TEXT PRIMARY KEY,
	title       TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	prompt      TEXT NOT NULL,
	repo_url    TEXT NOT NULL,
	base_ref    TEXT NOT NULL DEFAULT 'main',
	strategy    TEXT NOT NULL DEFAULT 'IMPLEMENT',
	agent_count INTEGER NOT NULL DEFAULT 1,
	priority    INTEGER NOT NULL DEFAULT 0,
	state       TEXT NOT NULL DEFAULT 'QUEUED',
	image       TEXT NOT NULL DEFAULT 'orchestrate-agent:latest',
	create_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	start_time  TEXT,
	end_time    TEXT
);

CREATE INDEX IF NOT EXISTS idx_tasks_state_priority
	ON tasks(state, priority DESC, create_time ASC);

CREATE TABLE IF NOT EXISTS runs (
	id          TEXT PRIMARY KEY,
	task_id     TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
	agent_index INTEGER NOT NULL DEFAULT 0,
	branch      TEXT NOT NULL DEFAULT '',
	state       TEXT NOT NULL DEFAULT 'PENDING',
	exit_code   INTEGER,
	output      TEXT NOT NULL DEFAULT '',
	log_path    TEXT NOT NULL DEFAULT '',
	create_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	start_time  TEXT,
	end_time    TEXT
);

CREATE INDEX IF NOT EXISTS idx_runs_task_id ON runs(task_id);

CREATE TABLE IF NOT EXISTS schedules (
	id            TEXT PRIMARY KEY,
	title         TEXT NOT NULL DEFAULT '',
	description   TEXT NOT NULL DEFAULT '',
	schedule_expr TEXT NOT NULL,
	schedule_type TEXT NOT NULL DEFAULT 'CRON',
	prompt        TEXT NOT NULL,
	repo_url      TEXT NOT NULL,
	base_ref      TEXT NOT NULL DEFAULT 'main',
	strategy      TEXT NOT NULL DEFAULT 'IMPLEMENT',
	agent_count   INTEGER NOT NULL DEFAULT 1,
	image         TEXT NOT NULL DEFAULT 'orchestrate-agent:latest',
	state         TEXT NOT NULL DEFAULT 'ACTIVE',
	last_run_time TEXT,
	next_run_time TEXT,
	run_count     INTEGER NOT NULL DEFAULT 0,
	max_runs      INTEGER NOT NULL DEFAULT 0,
	create_time   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_schedules_state_next
	ON schedules(state, next_run_time);

CREATE TABLE IF NOT EXISTS users (
	id           TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	email        TEXT NOT NULL DEFAULT '',
	state        TEXT NOT NULL DEFAULT 'ACTIVE',
	create_time  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	update_time  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email != '';

CREATE TABLE IF NOT EXISTS credentials (
	id              TEXT PRIMARY KEY,
	user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	credential_type TEXT NOT NULL,
	external_id     TEXT NOT NULL DEFAULT '',
	public_key      BLOB,
	metadata        TEXT NOT NULL DEFAULT '{}',
	create_time     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_credentials_user ON credentials(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_credentials_external
	ON credentials(credential_type, external_id) WHERE external_id != '';

CREATE TABLE IF NOT EXISTS sessions (
	id          TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	token_hash  TEXT NOT NULL,
	provider    TEXT NOT NULL DEFAULT '',
	ip_address  TEXT NOT NULL DEFAULT '',
	user_agent  TEXT NOT NULL DEFAULT '',
	expires_at  TEXT NOT NULL,
	create_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
	revoked_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash);

CREATE TABLE IF NOT EXISTS device_codes (
	device_code TEXT PRIMARY KEY,
	user_code   TEXT NOT NULL UNIQUE,
	client_id   TEXT NOT NULL DEFAULT '',
	scope       TEXT NOT NULL DEFAULT '',
	user_id     TEXT,
	state       TEXT NOT NULL DEFAULT 'PENDING',
	expires_at  TEXT NOT NULL,
	interval_s  INTEGER NOT NULL DEFAULT 5,
	create_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS auth_codes (
	code                  TEXT PRIMARY KEY,
	user_id               TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	client_id             TEXT NOT NULL DEFAULT '',
	redirect_uri          TEXT NOT NULL,
	scope                 TEXT NOT NULL DEFAULT '',
	code_challenge        TEXT NOT NULL,
	code_challenge_method TEXT NOT NULL DEFAULT 'S256',
	expires_at            TEXT NOT NULL,
	consumed              INTEGER NOT NULL DEFAULT 0,
	create_time           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS ciba_requests (
	auth_req_id     TEXT PRIMARY KEY,
	user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	client_id       TEXT NOT NULL DEFAULT '',
	scope           TEXT NOT NULL DEFAULT '',
	binding_message TEXT NOT NULL DEFAULT '',
	state           TEXT NOT NULL DEFAULT 'PENDING',
	expires_at      TEXT NOT NULL,
	interval_s      INTEGER NOT NULL DEFAULT 5,
	webhook_url     TEXT NOT NULL DEFAULT '',
	create_time     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
`
