# Cabure

Cabure is a minimal GitOps operator built in Go with controller-runtime and Helm-based installation.

## Install

```bash
helm upgrade --install cabure \
  ./charts/cabure \
  --namespace cabure-system \
  --create-namespace
```

For a published release:

```bash
helm upgrade --install cabure \
  oci://ghcr.io/greedykomododragon/charts/cabure \
  --version <version> \
  --namespace cabure-system \
  --create-namespace
```

## Values

Values under `spec.render.helm.valuesFiles` are resolved relative to the repository checkout root and must stay within the checkout. Use repo-root paths such as `infra/dev-overrides/...`; parent traversal like `../` is rejected.

## Git sources

Cabure accepts `https://`, `ssh://`, and scp-style SSH repository URLs such as `git@github.com:org/repo.git`.

For SSH sources, create a `kubernetes.io/ssh-auth` Secret in the same namespace as the `GitApplication` with:

- `ssh-privatekey`
- `known_hosts`

The operator loads the private key into an ephemeral file and uses the provided `known_hosts` data for strict host key verification.

Secrets are always read from the `GitApplication` namespace. The controller can still watch `GitApplication` resources cluster-wide, but credential access stays namespace-local.

Example secret helper:

```bash
hack/create-git-ssh-secret.sh \
  -n cabure-git-ssh \
  -N cabure-system \
  -o ./out \
  -k ./known_hosts
```
