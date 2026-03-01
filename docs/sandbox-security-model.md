# Sandbox Security Model (Draft)

This document proposes a capability-driven sandbox model for Orchestrate, designed for a public-facing, single-tenant deployment.

## Goals

- Keep agent execution useful while reducing blast radius.
- Make permissions explicit, auditable, and revocable.
- Support runtime grants similar to mobile platforms (`deny`, `allow once`, `allow for task`, `always`).
- Enforce least privilege over time (drop permissions when no longer needed).

## Threat Model

Assume agent code and prompts are potentially adversarial.

Primary risks:

- Secret exfiltration
- Lateral movement through network
- Data overreach on filesystem
- Supply-chain abuse through arbitrary container images
- Privilege persistence across runs

Out of scope for this model:

- Hypervisor/container escape zero-days
- Host compromise from unrelated workloads

## Core Concepts

### 1. Capability Manifest

Each task/schedule carries a manifest declaring required capabilities.

Example (`orchestrate.manifest.yaml`):

```yaml
version: 1
sandbox:
  image: orchestrate-agent:latest
  filesystem:
    - path: /workspace
      access: [read, write]
    - path: /workspace/tests
      access: [read]
  network:
    mode: restricted
    allow:
      - api.openai.com:443
      - api.anthropic.com:443
      - github.com:443
  secrets:
    allow:
      - OPENAI_API_KEY
  tools:
    allow:
      - git
      - go
  limits:
    cpu: "2"
    memory_mb: 2048
    pids: 512
```

### 2. Permission Grants

Grant decision states:

- `deny`
- `allow_once` (single use)
- `allow_for_task` (valid for current task execution graph)
- `allow_for_schedule_run` (for one schedule invocation)
- `allow_always` (persisted policy for this operator/instance)

Each grant should be persisted with:

- principal (`system`, operator)
- scope (task/schedule/backend)
- capability key
- expiration
- audit metadata

### 3. Permission Lifecycle (Least Privilege Drop)

Borrowing from BSD capability ideas and mobile runtime models:

- Start with minimal baseline capabilities.
- Temporarily elevate only for a declared phase.
- Drop back after phase completion.

Execution phases:

1. `bootstrap` (clone/fetch)
2. `build`
3. `test`
4. `review`
5. `publish` (if enabled)

Example:

- Network allowed in `bootstrap`, denied in `test`.
- Write access to `/workspace` in `build`, read-only in `review`.

### 4. Subset Filesystem Grants ("iOS photo picker" model)

Support path-scoped grants where the agent only sees granted paths.

Pattern:

- Task requests `filesystem.read` for `repo/subdir-a`.
- Policy engine issues scoped mount/bind rules.
- Agent process only sees granted subtree(s).

Result:

- No broad repo/global host visibility.
- Fine-grained, user-approved access to selected artifacts.

## Platform Inspiration and What to Borrow

### iOS (TCC + picker model)

Borrow:

- Explicit user consent for sensitive resources.
- Subset grants (selected items, not full collection).
- Strong default deny.

### Android (runtime permissions + SAF)

Borrow:

- Runtime grant prompts and revocation.
- Scoped storage and per-URI file access.

### WASI / capability systems

Borrow:

- No ambient authority.
- Explicit preopened directories and networking capabilities.

### Deno

Borrow:

- Declarative flags (`--allow-read`, `--allow-net=...`).
- Hostname/path-scoped permissions.

### V8 isolates

Borrow:

- Tight memory/time limits.
- Isolated execution compartments (useful long-term for non-shell tools).

### Docker / chroot / BSD jails

Borrow:

- Namespace/process/network isolation (Docker baseline today).
- Jail/chroot mindset of strict resource view boundaries.

## Proposed Architecture

- `Policy Compiler`: converts manifest + global policy into runtime sandbox config.
- `Permission Broker`: handles grant workflow and persistence.
- `Sandbox Driver`: applies policy to Docker today, pluggable later for WASI/isolates.
- `Audit Log`: append-only records of requested/approved/used capabilities.

## Rollout Plan

### Phase 0 (completed)

- Image allowlist enforcement (API + runtime)
- Optional network isolation mode (`default` / `none`)
- CI security scanning with `govulncheck` and `gosec`

### Phase 1 (partially completed)

- Manifest persistence on tasks/schedules (`manifest` JSON)
- Filesystem path scoping via sparse-checkout (repo subpath visibility)
- Filesystem write-scope enforcement via post-run changed-file policy checks
- Network policy modes in manifests (`default`, `none`, `allowlist`)
- Allowlist admission checks for required endpoints (repo host, configured API base URLs)

Remaining:

- Add first-class manifest schema to task/schedule APIs.
- Add hard egress enforcement for allowlist mode (kernel/network layer, not just admission checks).

### Phase 2

- Implement grant broker with `once/task/always` semantics.
- Add permission prompts for high-risk capabilities.
- Add grant expiration and revocation APIs.

### Phase 3

- Phase-based permission drop during execution.
- Per-agent differentiated capabilities in multi-agent strategies.
- Policy simulation mode (`dry-run`) and compliance checks.

## Recommended Defaults for Public-Facing Deployment

- `ORCHESTRATE_ALLOWED_IMAGES` set to a pinned internal image list.
- `ORCHESTRATE_ALLOW_ANY_IMAGE=false`.
- `ORCHESTRATE_SANDBOX_NETWORK=none` unless task explicitly needs egress.
- Outbound network policy at host/VPC layer even when sandbox policy exists.
- Short-lived API credentials with minimal scope.

## Open Design Questions

- Should grant prompts be interactive-only, or support policy-as-code auto-approval?
- How should grants propagate in adversarial/code-and-test handoff workflows?
- Should schedules inherit prior grants automatically, or require per-run renewal?
- Do we need per-repo trust tiers with stricter defaults for untrusted repos?
