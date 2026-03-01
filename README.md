# Orchestrate

`orchestrate` is a Go service + CLI for running coding agents against Git repositories in isolated Docker sandboxes.

It is designed for a single-tenant operator model (one team/org running its own instance), with API-first task execution and scheduling.

## What It Is

Orchestrate gives you:

- An HTTP API server for task and schedule management
- A CLI for day-to-day operations (`submit`, `list`, `status`, `logs`, `schedule`, `auth`)
- A scheduler that dequeues queued work and triggers scheduled work
- Strategy-based orchestration (`IMPLEMENT`, `INVESTIGATE`, `COMPETE`, `BATCH`, `ADVERSARIAL`, `CODE_AND_TEST`)
- Backend selection per task/schedule (`claude` and `codex`)
- Persistent state in SQLite (tasks, runs, schedules, users, sessions, auth artifacts)

## What It Can Do

- Run one-off coding tasks against a repo + base branch
- Fan out work across multiple agents in parallel (`COMPETE` / `BATCH`)
- Run adversarial review mode (`ADVERSARIAL`) with implementer + reviewer roles (sequential shared-workspace handoff)
- Split work into code + tests roles (`CODE_AND_TEST`) with sequential shared-workspace handoff
- Run recurring jobs using cron or ISO-8601 intervals
- Keep per-run execution logs and run state history
- Authenticate via static bearer token (bootstrap/admin)
- Authenticate via JWT access/refresh tokens
- Authenticate via GitHub / Google token exchange
- Optionally enable WebAuthn
- Optionally enable device + browser auth-code flows (explicitly disabled by default)
- Route work to common agent backends: `claude` (`anthropic` alias) and `codex` (`openai` alias)
- Attach sandbox permission manifests (filesystem scope + network policy)

## How To Use It

### 1. Prerequisites

- Go `1.24+`
- Docker
- Access to at least one agent CLI in your container image (`claude` and/or `codex`)

### 2. Build the CLI

```bash
go build -o orchestrate ./cmd/orchestrate
```

### 3. Build an agent image

Default image build:

```bash
docker build -t orchestrate-agent:latest -f docker/Dockerfile.agent .
```

Notes:

- The default `Dockerfile.agent` installs Claude Code CLI.
- If you want `--agent codex`, use an image that includes `codex` as well, then pass it with `--image`.

### 4. Start the server

```bash
./orchestrate server --addr :8080
```

On first start, Orchestrate creates a data directory and bootstrap token:

- Data dir default: `~/.local/share/orchestrate`
- Token file: `~/.local/share/orchestrate/token`
- JWT key: `~/.local/share/orchestrate/jwt.key`

### 5. Authenticate from another terminal

```bash
export ORCHESTRATE_TOKEN="$(cat ~/.local/share/orchestrate/token)"
./orchestrate auth login --method token --with-token "$ORCHESTRATE_TOKEN"
```

### 6. Submit work

```bash
./orchestrate submit \
  --title "Fix flaky test" \
  --agent claude \
  --prompt "Find and fix flaky tests, then explain the root cause." \
  --repo https://github.com/your-org/your-repo.git \
  --base-ref main \
  --strategy IMPLEMENT \
  --agents 1 \
  --fs-paths "cmd,internal" \
  --network-mode allowlist \
  --egress-domains "github.com:443,api.anthropic.com:443"
```

### 7. Inspect progress

```bash
./orchestrate list
./orchestrate status <task_id>
```

If a task name is `tasks/abc123`, the task id is `abc123`.

For logs, first get run IDs from the API:

```bash
curl -s -H "Authorization: Bearer $ORCHESTRATE_TOKEN" \
  http://localhost:8080/v1/tasks/<task_id>/runs
```

Then stream logs:

```bash
./orchestrate logs --task <task_id> --run <run_id>
```

### 8. Create recurring jobs

Cron example:

```bash
./orchestrate schedule create \
  --title "Hourly dependency check" \
  --agent claude \
  --schedule "0 * * * *" \
  --prompt "Check for vulnerable dependencies and propose updates." \
  --repo https://github.com/your-org/your-repo.git
```

Interval example:

```bash
./orchestrate schedule create \
  --schedule "R/PT1H" \
  --prompt "Run hourly code health checks." \
  --repo https://github.com/your-org/your-repo.git
```

Manage schedules:

```bash
./orchestrate schedule list
./orchestrate schedule pause <schedule_id>
./orchestrate schedule resume <schedule_id>
./orchestrate schedule delete <schedule_id>
```

### Useful Environment Variables

- `ORCHESTRATE_ADDR` - server bind address
- `ORCHESTRATE_MAX_CONCURRENT` - scheduler concurrency cap
- `ORCHESTRATE_DATA_DIR` - base data directory
- `ORCHESTRATE_SERVER` - CLI server URL
- `ORCHESTRATE_TOKEN` - static admin token or CLI override
- `ORCHESTRATE_AGENT` - default backend (`claude` or `codex`)
- `ORCHESTRATE_ALLOWED_IMAGES` - comma-separated container image allowlist (default: `orchestrate-agent:latest`)
- `ORCHESTRATE_ALLOW_ANY_IMAGE` - bypass image allowlist (`true`/`1`), useful for local development only
- `ORCHESTRATE_SANDBOX_NETWORK` - sandbox network mode (`default` or `none`)
- `ORCHESTRATE_ENABLE_EMAIL_AUTH` - enables insecure email-based auth flows (off by default)
- `ORCHESTRATE_WEBAUTHN_RPID`, `ORCHESTRATE_WEBAUTHN_RPNAME`, `ORCHESTRATE_WEBAUTHN_ORIGINS` - WebAuthn config
- `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL` - passed to Claude runs
- `OPENAI_API_KEY`, `OPENAI_BASE_URL` - passed to Codex runs

## How It Works

Core flow:

1. Client creates a task (or a schedule that later creates tasks).
2. Task is persisted in SQLite with `QUEUED` state.
3. Scheduler polls for queued tasks and due schedules.
4. Orchestrator picks a strategy and generates one or more `AgentPlan`s.
5. For each plan, orchestrator creates a run record.
6. Orchestrator creates a hardened Docker sandbox.
7. Orchestrator selects backend (`claude` or `codex`) per task.
8. Backend-specific API credentials are injected.
9. The agent CLI executes with the planned prompt.
10. Run state and output are persisted.
11. Strategy evaluates results and marks task `SUCCEEDED` or `FAILED`.

Main components:

- API server: `internal/api`
- Auth + token logic: `internal/auth`
- Scheduler + orchestration: `internal/orchestrator`
- Agent adapters: `internal/agent`
- Sandbox runtime: `internal/sandbox`
- Persistence: `internal/store`

## Sandbox Security

The Docker sandbox is hardened by default:

- Runs as non-root user `1000:1000`
- Drops all Linux capabilities (`--cap-drop ALL`)
- Enables `no-new-privileges`
- Uses read-only root filesystem (`--read-only`)
- Limits process count (`--pids-limit 512`)
- Writable paths are restricted to tmpfs mounts (`/tmp` and `/home/agent/workspace`)
- Only backend-relevant secrets are injected into a run (`ANTHROPIC_*` for Claude, `OPENAI_*` for Codex)
- Enforces image allowlist at API submit time and sandbox runtime
- Supports explicit network isolation mode (`ORCHESTRATE_SANDBOX_NETWORK=none`)
- Supports manifest-based sparse repo visibility (`manifest.sandbox.filesystem`)
- Supports manifest network policies (`default`, `none`, `allowlist`)

Additional auth/security controls:

- One-time consumption for auth code, device code, and CIBA grants
- Request body size limits and HTTP timeouts
- CIBA webhook URL validation + runtime DNS/IP checks against local/private targets
- Insecure email/browser/device auth flows are disabled unless explicitly enabled

Detailed roadmap: [`docs/sandbox-security-model.md`](docs/sandbox-security-model.md)

Important deployment notes for public-facing use:

- Agent execution is still arbitrary code execution by design.
- Run this service on dedicated infrastructure.
- Restrict outbound network egress at the host/VPC layer.
- Keep API keys scoped and minimal.
- Put the API behind HTTPS + a reverse proxy with rate limiting.

Manifest network `allowlist` currently enforces admission checks (required hosts must be declared) and audit env propagation. Full kernel-level egress filtering still requires host/VPC controls.

## Testing and CI/CD

Local checks:

```bash
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
gosec ./...
go test ./... -cover
```

GitHub Actions:

- CI: [`.github/workflows/ci.yml`](.github/workflows/ci.yml)
- CD: [`.github/workflows/cd.yml`](.github/workflows/cd.yml) (publishes `orchestrate-agent` image to GHCR)
