# Repository Guidelines

Go module for the `mieru` proxy client and `mita` proxy server. Entrypoints are `cmd/mieru` and `cmd/mita`, the public API is `apis/`, implementation is `pkg/`, and integration-test helpers and Dockerfiles are in `test/`.

## Dependency Layers

Dependencies flow downward only. Lower layers must not import `cmd/` or `pkg/cli`.

1. CLI: `cmd/mieru`, `cmd/mita`, `pkg/cli`.
2. Public API: `apis/client`, `apis/server`, plus shared `apis/common`, `apis/model`, `apis/constant`, `apis/log`, `apis/trafficpattern`. `apis/internal` is API-private.
3. Control and config: `pkg/appctl` (app status, config files, URL import/export, gRPC management). `pkg/appctl/appctlcommon` holds helpers that `apis/` can use without importing the gRPC-dependent `pkg/appctl`.
4. Runtime: `pkg/protocol` (mux/session transport; `serveruser` handles server-user auth), `pkg/socks5`, `pkg/cipher`, `pkg/replay`, `pkg/congestion`, `pkg/sockopts`, `pkg/egress`.
5. Support utilities: `pkg/common`, `pkg/log`, `pkg/stderror`, `pkg/metrics`, `pkg/version`, and other small helper packages.

## Compatibility Requirements

- Existing protocol and wire format must not change.
- Exported elements in `apis/` must not change unless absolutely necessary.
- Exported elements in `pkg/` may change, provided protocol and wire format remain unchanged.

## Generated Code

`pkg/appctl/appctlpb`, `pkg/appctl/appctlgrpc`, `pkg/metrics/metricspb`, and `pkg/version/updater/updaterpb` are generated. Never edit them by hand: change the `.proto` under `pkg/**/proto/` and run `make protobuf`.

## Build and Test

- `make lib`: fmt, vet, build, and race-enabled unit tests with coverage. Run before larger submissions.
- `make lint`: `golangci-lint` using `.golangci.yaml`. Not included in `make lib`.
- `make bench`: run when changing `pkg/cipher` or `pkg/protocol`.
- `make run-container-test`: Docker integration tests. Run when changes affect networking, client/server behavior, API clients/servers, or deployment configs. Takes a few minutes.
- Run tests outside the sandbox; most need network setup.
- Do not run `make clean` unless explicitly requested.
- Check `go.mod` before writing code. It pins the Go language version and dependency versions; do not use newer stdlib APIs or libraries that are not already present.

## Conventions

- Prefer table-driven tests for protocol, parsing, and config behavior.
- Avoid very long identifiers. If a concise name is not self-explanatory, add a brief Godoc comment.
- Use concise Conventional Commit prefixes.
- When modifying a doc that has a `*.zh_CN.md` counterpart, update the Chinese file with a precise translation of the changed content.
