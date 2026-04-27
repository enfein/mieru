# Repository Guidelines

## Project Structure & Module Organization

This is a Go module for the `mieru` proxy client and `mita` proxy server.

Entrypoints live in `cmd/mieru` and `cmd/mita`.

Public API packages are under `apis/`, while most implementation code is in `pkg/`.

Example and template configs are in `configs/`.

Installation, protocol, security, and operation docs are in `docs/`.

Integration-test helpers, Dockerfiles, and test commands are under `test/`.

Build and packaging metadata lives in `build/`, `deployments/`, and the root `Makefile`.

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
