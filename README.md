# Cabure

Cabure is a minimal GitOps operator for a single Kubernetes cluster. It watches `GitApplication` resources, checks out a Git repository, renders plain YAML or a local Helm chart, applies the result with Kubernetes Server-Side Apply, and prunes objects removed from Git using a stored inventory.

Cabure is intentionally small. It is meant for teams that want a Git-to-cluster reconciliation loop without taking on the scope of a larger platform.

## Status

Cabure is usable, but it is still an early project. The supported install surface is the Helm chart in [`charts/cabure`](./charts/cabure). Generated assets under `config/` exist to support CRD and RBAC generation; they are not the primary user interface.

## Features

- Reconciles a namespaced `GitApplication` custom resource.
- Supports Git sources over `https://`, `ssh://`, and scp-style SSH URLs.
- Renders either plain Kubernetes YAML or a local Helm chart from the checked out repository.
- Applies resources with Server-Side Apply.
- Tracks managed inventory and prunes stale resources when `spec.prune` is enabled.
- Restricts credentials lookup to the `GitApplication` namespace.
- Exposes Prometheus metrics and health/readiness probes.
- Ships with a Helm chart for install and upgrade.

## Non-goals

Cabure does not try to be a full Argo CD replacement. It does not currently provide:

- A web UI or public API.
- Multi-cluster management.
- Kustomize, Jsonnet, CUE, or arbitrary plugin-based rendering.
- Remote Helm values files or OCI chart sources for user applications.
- Cross-namespace secret access.

## Architecture

The reconciliation loop is:

1. Read a `GitApplication`.
2. Validate the spec and access policy.
3. Check out the requested repository revision into the local cache.
4. Render either YAML manifests or a local Helm chart from the checkout.
5. Normalize and validate objects against the destination namespace and cluster-scope policy.
6. Apply objects with Server-Side Apply.
7. Diff the recorded inventory against the desired inventory and prune stale objects that Cabure previously marked as managed.
8. Update status conditions, revision fields, and the managed inventory.

Pruning is guarded by Cabure’s management metadata. An object is only deleted if it appears in the previous inventory and still carries Cabure’s application UID annotation and managed-by label.

For more detail, see [docs/architecture.md](./docs/architecture.md).

## Installation

### Prerequisites

- Kubernetes cluster access with privileges to install CRDs, RBAC, and the operator.
- Helm 3.
- A Cabure image reachable by your cluster. Set `image.repository` and `image.tag` in the Helm chart values for your environment.

### Install from the local chart

```bash
helm upgrade --install cabure \
  ./charts/cabure \
  --namespace cabure-system \
  --create-namespace
```

## Quick Start

### 1. Install Cabure

Install the chart using one of the commands above.

### 2. Create Git credentials

For public HTTPS repositories, no secret is required.

For SSH repositories, create a `kubernetes.io/ssh-auth` secret in the same namespace as the `GitApplication`. Cabure expects:

- `ssh-privatekey`
- `known_hosts`

Helper script:

```bash
hack/create-git-ssh-secret.sh \
  -n cabure-demo-git \
  -N cabure-system \
  -o ./out \
  -k ./known_hosts
```

Then apply the generated manifest:

```bash
kubectl apply -f ./out/cabure-demo-git.secret.yaml
```

### 3. Apply a `GitApplication`

Generic example:

```bash
kubectl apply -f config/samples/demo-gitapplication.yaml
```

Example resource:

```yaml
apiVersion: gitops.cabure.io/v1alpha1
kind: GitApplication
metadata:
  name: demo
  namespace: cabure-system
spec:
  source:
    repository: https://github.com/example/platform-config.git
    revision: main
    path: apps/demo
  destination:
    namespace: demo
  render:
    type: yaml
  interval: 1m
  prune: true
```

### 4. Verify reconciliation

```bash
kubectl get gitapplications -A
kubectl describe gitapplication demo -n cabure-system
```

The CRD exposes useful status columns:

- `Revision` from `status.appliedRevision`
- `Ready` from the `Ready` condition

Cabure also records:

- `status.attemptedRevision`
- `status.lastAttemptTime`
- `status.lastSuccessTime`
- `status.inventory`

## API Overview

`GitApplication.spec` contains:

- `source.repository`, `source.revision`, `source.path`, `source.secretRef`
- `destination.namespace`
- `render.type`
- `render.helm.releaseName`, `render.helm.valuesFiles`, `render.helm.includeCRDs`
- `allowedClusterScopedKinds`
- `takeoverExistingResources`
- `interval`
- `prune`
- `suspend`

Supported render modes:

- `yaml`
- `helm`

## Security and Safety Constraints

These constraints are part of Cabure’s contract and should be understood before use:

- Secrets are always read from the `GitApplication` namespace, even when the operator watches cluster-wide.
- `source.path` must stay within the checked out repository.
- `render.helm.valuesFiles` must be clean repository-relative paths and cannot escape the checkout root.
- Namespaced resources must either omit `metadata.namespace` or match `spec.destination.namespace`.
- Cluster-scoped resources are blocked unless the operator enables them and the application explicitly allows supported kinds.
- Supported cluster-scoped kinds are limited to `Namespace`, `CustomResourceDefinition`, `ClusterRole`, and `ClusterRoleBinding`.
- Managed object size is limited to 1 MiB after normalization.
- `spec.suspend` stops reconciliation and leaves the application in a non-ready suspended state.

## Helm Chart Values

Important chart values are defined in [`charts/cabure/values.yaml`](./charts/cabure/values.yaml):

- `image.repository`, `image.tag`
- `leaderElection.enabled`
- `operator.watchNamespace`
- `operator.concurrentReconciles`
- `operator.minimumRequeueInterval`
- `operator.allowClusterScopedResources`
- `operator.allowedRepositoryPrefixes`
- `operator.fieldManager`
- `operator.cacheDir`
- `cache.persistence.*`
- `metrics.serviceMonitor.enabled`

Validate your changes with:

```bash
helm lint charts/cabure
helm template cabure charts/cabure --namespace cabure-system
```

## Development

Common commands:

```bash
make test
make helm-lint
make helm-template
make manifests
make helm-sync
```

If your environment has limited system temp space, run tests with workspace-local cache and temp directories:

```bash
TMPDIR=$(pwd)/.gotmp \
GOCACHE=$(pwd)/.cache/go-build \
GOMODCACHE=$(pwd)/.cache/go-mod \
go test ./...
```

Contribution and release process notes are in:

- [CONTRIBUTING.md](./CONTRIBUTING.md)
- [docs/releasing.md](./docs/releasing.md)
- [CHANGELOG.md](./CHANGELOG.md)

## License

Cabure is licensed under the GNU Affero General Public License v3.0 only. See [LICENSE](./LICENSE).

If you modify Cabure and make that modified version available for users to interact with over a network, AGPLv3 requires you to offer the corresponding source code of that modified version to those users.
