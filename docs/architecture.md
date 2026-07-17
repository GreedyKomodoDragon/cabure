# Cabure Architecture

Cabure implements a deliberately small GitOps loop around one custom resource: `GitApplication`.

## Reconciliation Flow

1. The operator watches `gitapplications.gitops.cabure.io`.
2. A reconcile worker validates the requested repository, render mode, namespace, interval, and cluster-scope policy.
3. The repository is checked out into the local cache directory.
4. Cabure renders one of:
   - Plain YAML from `spec.source.path`
   - A local Helm chart from `spec.source.path`
5. Rendered objects are normalized before apply:
   - Namespace defaults to `spec.destination.namespace`
   - Namespace mismatches are rejected
   - Cluster-scoped objects are rejected unless explicitly enabled
   - Cabure adds ownership metadata used for inventory and pruning
6. Objects are applied with Kubernetes Server-Side Apply.
7. If pruning is enabled, Cabure compares the previous inventory with the newly desired inventory and deletes stale objects that still carry Cabure management metadata.
8. The operator updates status with the attempted revision, applied revision, timestamps, conditions, and normalized inventory.

## Status Model

Cabure writes three main conditions:

- `Ready`
- `Reconciling`
- `Stalled`

On a successful reconcile:

- `Ready=True`
- `Reconciling=False`
- `Stalled=False`

On failure:

- `Ready=False`
- `Reconciling=False`
- `Stalled` depends on whether the failure is treated as stalled

When suspended:

- `Ready=False`
- `Reconciling=False`
- Status message indicates reconciliation is suspended

## Rendering Model

Cabure supports two render types:

- `yaml`: read Kubernetes manifests from the checked out repository path
- `helm`: load and render a Helm chart from the checked out repository path

For Helm:

- `valuesFiles` are resolved relative to the repository checkout root
- absolute paths are rejected
- parent traversal outside the checkout root is rejected

## Credentials Model

Cabure reads credentials only from the namespace that contains the `GitApplication`.

Supported secret shapes:

- SSH auth via `kubernetes.io/ssh-auth` or a secret containing `ssh-privatekey` and `known_hosts`
- HTTPS auth via `username` plus `password`, or `token`

This keeps a cluster-wide watch from becoming cluster-wide secret access.

## Safety Boundaries

- Cabure only prunes objects it previously marked as managed.
- Namespaced objects cannot escape `spec.destination.namespace`.
- Cluster-scoped resources require both operator-level enablement and per-application allowlisting.
- Supported cluster-scoped kinds are intentionally limited.
- Object payloads larger than 1 MiB are rejected before apply.
