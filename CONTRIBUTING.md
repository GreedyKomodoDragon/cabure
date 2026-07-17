# Contributing to Cabure

Thanks for contributing.

## Development Setup

Prerequisites:

- Go
- Helm 3
- Access to a Kubernetes cluster if you want to exercise the chart manually

Clone the repository and run:

```bash
make test
make helm-lint
```

If your machine has limited temp space, prefer:

```bash
TMPDIR=$(pwd)/.gotmp \
GOCACHE=$(pwd)/.cache/go-build \
GOMODCACHE=$(pwd)/.cache/go-mod \
go test ./...
```

## Common Commands

```bash
make manifests
make helm-sync
make helm-template
make install
make uninstall
```

## Change Expectations

- Keep the supported install path centered on the Helm chart.
- If you change CRDs or RBAC generation sources, run `make manifests` and `make helm-sync`.
- If you change user-facing behavior, update `README.md` and any relevant docs under `docs/`.
- Add or update tests when changing reconciliation, validation, rendering, apply, or pruning behavior.

## Pull Requests

Before opening a PR:

1. Run tests and Helm validation locally.
2. Review generated or synced artifacts under `config/` and `charts/`.
3. Update `CHANGELOG.md` under `Unreleased` for user-visible changes.

## License

By contributing to Cabure, you agree that your contributions are licensed under `AGPL-3.0-only`.

Cabure is network-facing operator software. If you deploy a modified version for remote users, AGPLv3 requires offering the corresponding source code of that modified version to those users.
