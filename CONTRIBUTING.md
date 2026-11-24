# Contributing to Recall MCP Gateway

Thank you for your interest in contributing! This guide covers everything you need to get started.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to uphold this code.

## CLA Requirement

All contributors must sign a Contributor License Agreement (CLA) before their first PR can be merged. The CLA bot will prompt you automatically on your first pull request. This is a one-time requirement.

The CLA grants ControlisAI the right to include your contributions in the commercial Recall Platform under a separate license. This is standard practice for AGPL open-core projects (similar to Grafana, GitLab, etc.).

## Development Setup

### Prerequisites

- Go 1.26+
- Node.js 20+ (for dashboard development)
- Make

### Build & Run

```bash
git clone https://github.com/controlisai/recall-mcp-gateway.git
cd recall-mcp-gateway

# Build
make build

# Run with default config
make run

# Dashboard at http://localhost:8090/dashboard
# MCP endpoint at http://localhost:8090/mcp
```

### Run Tests

```bash
make test                # unit tests (with race detector)
make test-integration    # integration tests
make test-all            # both
make lint                # golangci-lint (if installed)
```

### Dashboard Development

The React dashboard lives in `web/`. Hot-reload development:

```bash
# Terminal 1: run the gateway
make run

# Terminal 2: Vite dev server (proxies API to :8090)
make web-dev
# Dashboard at http://localhost:5173
```

Build the dashboard into the Go binary:

```bash
make build-full    # builds React + Go in one step
```

## Contribution Zones

| Zone | Scope | Community PRs? |
|------|-------|---------------|
| **Core** | rules, baselines, detection, audit, dashboard | Welcome |
| **Auth providers** | new upstream auth types | Welcome |
| **Integrations** | new upstream MCP server support | Welcome |
| **Platform bridge** | `internal/engine/`, `internal/platform/` | Maintainer only |
| **Protocol** | MCP spec compliance, JSON-RPC | Discuss first |

For **protocol** changes, please open an issue to discuss before submitting a PR.

## How to Contribute

### Bug Reports

1. Search [existing issues](https://github.com/controlisai/recall-mcp-gateway/issues) first
2. Use the **Bug Report** issue template
3. Include: Go version, OS, config (redact secrets), steps to reproduce, expected vs actual behavior

### Feature Requests

1. Open an issue using the **Feature Request** template
2. Describe the use case, not just the solution
3. Label suggestions: `area/core`, `area/dashboard`, `area/auth`, `area/protocol`

### Pull Requests

1. Fork the repo and create a branch from `main`
2. Write your code following the standards below
3. Add or update tests for your changes
4. Run `make test` and `make lint` — both must pass
5. Submit a PR using the PR template

### Commit Messages

Follow conventional commits:

```
type: short description

Longer explanation if needed.
```

Types: `feat`, `fix`, `docs`, `chore`, `test`, `refactor`, `perf`

Examples:
- `feat: add webhook retry with exponential backoff`
- `fix: handle nil arguments in consent retry flow`
- `docs: add PostgreSQL deployment example`

## Coding Standards

### Go

- Go 1.26+, standard library preferred over external dependencies
- HTTP: stdlib `net/http` with `http.ServeMux` (not Fiber, not chi)
- Error wrapping: `fmt.Errorf("context: %w", err)`
- No `log.Fatal` outside `main()`
- Tests in every package, `go test -race` clean
- Exported types at package level, internal helpers unexported

### React Dashboard (web/)

- TypeScript strict mode
- Zustand for state management (not Redux)
- CSS custom properties for theming (not Tailwind utility classes for layout)
- Minimum 32px touch targets for clickable elements

### Tests

- Table-driven tests for Go
- Every new API handler needs a response shape test
- Every new page component needs a render smoke test
- Run `go test -race ./internal/...` before submitting

## Review Process

1. All PRs require one review from a maintainer
2. CI must pass (build, test, lint)
3. CLA must be signed
4. PRs are typically reviewed within 48 hours

## Getting Help

- **Questions:** Open a [Discussion](https://github.com/controlisai/recall-mcp-gateway/discussions) (Q&A category)
- **Bugs:** Open an [Issue](https://github.com/controlisai/recall-mcp-gateway/issues)
- **Security:** See [SECURITY.md](SECURITY.md)
