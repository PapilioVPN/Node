# Contributing to PapilioVPN Node

Use [issues](https://github.com/papilio-platform/Node/issues) for reproducible
problems and [pull requests](https://github.com/papilio-platform/Node/pulls) for
focused changes. Describe the resulting behavior and how you verified it.

## Before submitting

- Use Go 1.26 or newer and keep changes focused on a coherent responsibility.
- Preserve typed contracts, cancellation, error meaning and resource ownership.
- Add meaningful behavior tests for implemented capabilities. Identify whether
  verification uses controlled inputs or real isolated engine/OS integration.
- Keep code, comments and commit/PR metadata in English. Update both
  [English](README.md) and [Persian](README.fa.md) operational instructions when
  their corresponding behavior changes.
- Keep credentials, runtime configuration, generated binaries and private work
  plans out of commits.

Run:

```bash
gofmt -w cmd internal
go mod tidy
go mod verify
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o bin/node ./cmd/node
```

The [CI workflow](.github/workflows/ci.yml) declares the pinned compiler/scanner
and verifies native Linux amd64 and arm64 builds.

## Reporting a problem

Include `node version --json`, the command or operation, expected/actual results
and steps to reproduce. Remove secrets and private user data from any examples.
Distinguish a confirmed result from an untested assumption.
