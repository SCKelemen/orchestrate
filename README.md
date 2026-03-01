# orchestrate

Parallel Claude Code agent orchestration with Docker-based isolation, a SQLite task queue, and an HTTP API.

## Overview

orchestrate runs multiple [Claude Code](https://docs.anthropic.com/en/docs/claude-code) CLI agents in parallel, each in an isolated Docker container with its own git worktree. It provides:

- **Task queue** with priority scheduling and state management
- **Multiple execution strategies** for different use cases
- **Cron and interval scheduling** for recurring tasks
- **HTTP API** following [AIP](https://aip.dev) resource-oriented design
- **OAuth 2.0 authentication** with multiple grant types

## Architecture

```
                                +-----------+
                                |  CLI /    |
                                |  Client   |
                                +-----+-----+
                                      |
                                      v
+--------+   HTTP   +----------+   Dequeue   +-------------+
| Sched- | ------> |  API     | ---------> | Orchestrator |
| uler   |         |  Server  |            +------+------+
+--------+         +----------+                   |
                                            Plan (Strategy)
                                                  |
                                     +------------+------------+
                                     |            |            |
                                     v            v            v
                                 +-------+    +-------+    +-------+
                                 |Docker |    |Docker |    |Docker |
                                 |  #0   |    |  #1   |    |  #2   |
                                 |claude |    |claude |    |claude |
                                 +-------+    +-------+    +-------+
```

## Quick Start

### Prerequisites

- Go 1.24+
- Docker
- An `ANTHROPIC_API_KEY` for Claude Code

### Build

```bash
go build -o orchestrate ./cmd/orchestrate
```

### Build the Agent Image

```bash
docker build -t orchestrate-agent:latest -f docker/Dockerfile.agent .
```

### Run the Server

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export ORCHESTRATE_TOKEN=my-secret-token

./orchestrate server --addr :8080
```

### Submit a Task

```bash
./orchestrate submit \
  --prompt "Add input validation to the /api/users endpoint" \
  --repo https://github.com/org/repo \
  --strategy IMPLEMENT
```

### Check Status

```bash
./orchestrate list
./orchestrate status <task-id>
./orchestrate logs <task-id> <run-id>
```

## CLI Reference

| Command | Description |
|---|---|
| `orchestrate server` | Start the HTTP API server |
| `orchestrate submit` | Submit a new task |
| `orchestrate list` | List tasks |
| `orchestrate status` | Get task status and runs |
| `orchestrate cancel` | Cancel a running or queued task |
| `orchestrate logs` | Stream run logs |
| `orchestrate schedule` | Manage recurring schedules |
| `orchestrate auth` | Authentication management |

## API Reference

The HTTP API follows [AIP](https://aip.dev) resource-oriented design conventions.

### Tasks

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/tasks` | Create a task |
| `GET` | `/v1/tasks` | List tasks |
| `GET` | `/v1/tasks/{id}` | Get a task |
| `PATCH` | `/v1/tasks/{id}` | Update a task |
| `DELETE` | `/v1/tasks/{id}` | Delete a task |
| `POST` | `/v1/tasks/{id}/:cancel` | Cancel a task |
| `POST` | `/v1/tasks/{id}/:retry` | Retry a failed task |

### Runs

| Method | Path | Description |
|---|---|---|
| `GET` | `/v1/tasks/{id}/runs` | List runs for a task |
| `GET` | `/v1/tasks/{id}/runs/{rid}` | Get a run |
| `GET` | `/v1/tasks/{id}/runs/{rid}/:logs` | Stream run logs (SSE) |

### Schedules

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/schedules` | Create a schedule |
| `GET` | `/v1/schedules` | List schedules |
| `GET` | `/v1/schedules/{id}` | Get a schedule |
| `PATCH` | `/v1/schedules/{id}` | Update a schedule |
| `DELETE` | `/v1/schedules/{id}` | Delete a schedule |
| `POST` | `/v1/schedules/{id}/:pause` | Pause a schedule |
| `POST` | `/v1/schedules/{id}/:resume` | Resume a schedule |

### Users

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/users` | Create a user |
| `GET` | `/v1/users` | List users |
| `GET` | `/v1/users/{id}` | Get a user |
| `PATCH` | `/v1/users/{id}` | Update a user |
| `DELETE` | `/v1/users/{id}` | Delete a user |

### Auth

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/auth/token` | Token endpoint (multiple grant types) |
| `POST` | `/v1/auth/token/:revoke` | Revoke a refresh token |
| `GET` | `/v1/auth/userinfo` | Get current user info |
| `GET` | `/v1/auth/authorize` | Authorization code login page |
| `POST` | `/v1/auth/authorize` | Authorization code form submit |
| `POST` | `/v1/auth/device` | Initiate device flow |
| `GET` | `/v1/auth/device/verify` | Device verification page |
| `POST` | `/v1/auth/ciba` | Initiate CIBA flow |

## Execution Strategies

| Strategy | Agents | Success Criteria | Use Case |
|---|---|---|---|
| **IMPLEMENT** | 1 | Agent exits 0 | Standard single-agent task |
| **INVESTIGATE** | 1 (read-only) | Agent exits 0, output is summary | Analysis without code changes |
| **COMPETE** | N (default 2) | Any agent exits 0 | Race multiple approaches |
| **BATCH** | N (default 1) | All agents exit 0 | Fan-out parallel work |

## Authentication

The API supports multiple OAuth 2.0 grant types:

- **Bearer token** &mdash; static token via `ORCHESTRATE_TOKEN` env var, exchangeable for JWT
- **Authorization code + PKCE** &mdash; browser-based login flow ([RFC 7636](https://datatracker.ietf.org/doc/html/rfc7636))
- **Device flow** &mdash; CLI-friendly login ([RFC 8628](https://datatracker.ietf.org/doc/html/rfc8628))
- **CIBA** &mdash; backchannel authentication ([OpenID CIBA](https://openid.net/specs/openid-client-initiated-backchannel-authentication-core-1_0.html))
- **Token exchange** &mdash; GitHub/Google IdP tokens ([RFC 8693](https://datatracker.ietf.org/doc/html/rfc8693))
- **WebAuthn** &mdash; passkey registration and login

All API endpoints (except auth and health) require a `Bearer` token in the `Authorization` header.

## Configuration

| Variable | Description | Default |
|---|---|---|
| `ORCHESTRATE_DATA_DIR` | Data directory for SQLite DB | `~/.local/share/orchestrate` |
| `ANTHROPIC_API_KEY` | Claude API key (passed to agent containers) | &mdash; |
| `ORCHESTRATE_TOKEN` | Static bearer token for API auth | &mdash; |

## Docker Agent Image

The agent image (`docker/Dockerfile.agent`) provides the execution environment for Claude Code:

- Ubuntu 24.04 base
- Git, curl, jq
- Node.js 22 (required by Claude Code CLI)
- Claude Code CLI (`@anthropic-ai/claude-code`)
- Non-root `agent` user

Build it:

```bash
docker build -t orchestrate-agent:latest -f docker/Dockerfile.agent .
```

## Development

```bash
go build ./...
go test ./...
go vet ./...
```

## License

See [LICENSE](LICENSE) for details.
