# Repository Guidelines

## Project Structure & Module Organization

This is a Go module for the `mieru` proxy client and `mita` proxy server.

Entrypoints live in `cmd/mieru` and `cmd/mita`.

Public API packages are under `apis/`, while most implementation code is in `pkg/`.

Example and template configs are in `configs/`.

Installation, protocol, security, and operation docs are in `docs/`.

Integration-test helpers, Dockerfiles, and test commands are under `test/`.

Build and packaging metadata lives in `build/`, `deployments/`, and the root `Makefile`.

## Package Overview & Dependency Layers

Keep executable and CLI dependencies at the top of the graph. Shared API packages may be imported by both external users and internal `pkg/` code, but lower-level packages should not import `cmd/` or `pkg/cli`.

- Entrypoints and CLI: `cmd/mieru`, `cmd/mita`, and `pkg/cli` register and parse commands, load configuration, run daemons, and call management APIs.
- API surface: `apis/client` and `apis/server` expose embeddable client/server APIs. `apis/common`, `apis/model`, `apis/constant`, `apis/log`, and `apis/trafficpattern` provide shared interfaces, SOCKS/address models, logging hooks, and traffic-pattern helpers used by both API consumers and internal packages. `apis/internal` is for API-private helpers only.
- Control and configuration: `pkg/appctl` owns app status, profile/config file handling, URL import/export, and gRPC management services. `pkg/appctl/appctlcommon` contains helpers that API packages can use without importing the full gRPC-dependent `pkg/appctl`.
- Runtime implementation: `pkg/protocol`, `pkg/socks5`, `pkg/cipher`, `pkg/replay`, `pkg/congestion`, `pkg/sockopts`, and `pkg/egress` implement the mux/session transport, SOCKS5 handling, encryption, replay protection, congestion state, socket options, and egress action policy.
- Support packages: `pkg/common`, `pkg/log`, `pkg/stderror`, `pkg/metrics`, `pkg/version`, `pkg/version/updater`, `pkg/rng`, `pkg/mathext`, `pkg/deque`, and `pkg/testtool` provide reusable utilities, logging, errors, telemetry, version/update support, random/math helpers, data structures, and test support.
- Generated protobuf packages: `pkg/appctl/appctlpb`, `pkg/appctl/appctlgrpc`, `pkg/metrics/metricspb`, and `pkg/version/updater/updaterpb` are generated from `.proto` definitions and should not be edited by hand.

## Build, Test, and Development Commands

- `make fmt`: runs `go fmt ./...`.
- `make vet`: runs `go vet ./...`.
- `make lint`: runs `golangci-lint run ./...` using `.golangci.yaml`.
- `make lib`: formats, vets, builds all Go packages, runs race-enabled unit tests, and writes `coverage.out` / `coverage.html`.
- `make test-binary`: builds local binaries used by integration tests into `bin/`.
- `make run-container-test`: builds Docker test images and runs the integration tests.
- `make protobuf`: regenerates Go code after editing files in `pkg/**/proto/`.

## Coding Style & Naming Conventions

Use idiomatic Go formatted by `go fmt`; keep tabs for Go indentation.

Package names are short, lowercase, and domain-oriented.

Test files use the standard `*_test.go` pattern beside the package they cover.

Keep generated protobuf output in sync with `.proto` changes.

## Testing Guidelines

Add unit tests near changed code and prefer table-driven tests for protocol, parsing, and config behavior.

Run `make lib` before larger submissions.

Run `make bench` when changing `pkg/cipher`.

Use Docker integration tests when changes affect runtime networking, client/server behavior, API clients, or deployment configs. It can take a few minutes to run the Docker integration tests.

## Commit & Pull Request Guidelines

Use concise Conventional Commit-style prefixes.

## Agent-Specific Instructions

Do not run destructive cleanup such as `make clean` unless explicitly requested.

Avoid editing generated protobuf files by hand; update the source `.proto` and run `make protobuf` instead.

When modifying documentation, also provide a precise Chinese translation of the modified content in related `zh_CN.md` file.
