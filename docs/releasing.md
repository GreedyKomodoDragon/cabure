# Releasing Cabure

Use this checklist before publishing a new image or chart release.

## Validation

Run:

```bash
make test
make helm-lint
make helm-template
make manifests
make helm-sync
git diff --exit-code config charts
```

If local temp space is constrained:

```bash
TMPDIR=$(pwd)/.gotmp \
GOCACHE=$(pwd)/.cache/go-build \
GOMODCACHE=$(pwd)/.cache/go-mod \
go test ./...
```

## Versioning

Update release metadata consistently:

- `charts/cabure/Chart.yaml`
- Container image tag used for the release
- `CHANGELOG.md`

Ensure the README install examples match the published chart and image coordinates.

## Publication Checks

- Confirm CRDs in `charts/cabure/crds/` match `config/crd/bases/`.
- Confirm RBAC in `charts/cabure/files/rbac.yaml` matches `config/rbac/role.yaml`.
- Confirm sample manifests are generic and valid.
- Confirm no unreleased owner-specific or environment-specific references remain in docs.
- Confirm CI is green on the release commit.

## Release Artifacts

- Push the Cabure image.
- Publish the chart version.
- Tag the release in version control.
- Attach release notes based on `CHANGELOG.md`.
