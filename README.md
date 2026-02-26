# Portage

Portage is a Kubernetes operator that declaratively syncs container images between registries. Users define `ImageSync` custom resources that specify source images, destination registries, schedules, and authentication -- and the operator handles the rest.

The name comes from the act of carrying cargo between two bodies of water, which is exactly what this does: carrying container images between two registries.

## Why Portage

There is no CRD-native operator in the CNCF/Kubernetes ecosystem for declarative registry-to-registry image relocation. The existing alternatives each fall short in different ways:

- **ECR pull-through cache** -- AWS-native, but does not support arbitrary private registries (e.g., Chainguard's `cgr.dev`) and is unavailable in GovCloud regions.
- **dregsy** -- The closest functional match. Syncs images between registries using skopeo, but is config-file driven, not CRD-driven. There is no `kubectl get imagesync`.
- **kube-image-keeper (kuik)** -- Caches images into an in-cluster registry for availability purposes. Does not push to external registries like ECR.
- **Flux image automation** -- Watches registries for new tags and updates Git manifests. Does not copy images between registries.
- **CI/CD pipelines** -- Functional, but couples image relocation to CI system availability and does not provide Kubernetes-native status reporting.

Portage fills this gap with a Kubernetes-native, GitOps-friendly, CRD-driven approach that works with any OCI-compliant registry.

## Primary Use Case

The immediate use case driving this project is relocating paid/private Chainguard container images from `cgr.dev` into Amazon ECR in a DoD AWS GovCloud environment. However, Portage is designed to be registry-agnostic from the ground up -- it works with any OCI-compliant source and destination registry through a pluggable authentication interface.

## How It Works

Portage introduces a single custom resource, `ImageSync`, that declares which images to sync, where to pull them from, where to push them to, and on what schedule. The controller watches for these resources and handles the full lifecycle:

1. **Authenticate** to the source and destination registries using Kubernetes Secrets (`kubernetes.io/dockerconfigjson`) or cloud-native methods (IRSA for ECR, workload identity for GCR).
2. **Compare digests** between source and destination using lightweight HTTP HEAD requests. If the manifest digests match, the image is already up-to-date and the copy is skipped entirely.
3. **Copy** images that are new or have changed, transferring layers directly between registries without pulling them to the controller pod.
4. **Report status** per-image and per-tag on the `ImageSync` resource, including digests, sync timestamps, error messages, and summary counts. Standard Kubernetes conditions (`Ready`, `Syncing`) provide at-a-glance health.
5. **Emit events** (`ImageSynced`, `ImageSkipped`, `SyncFailed`, `SyncComplete`) visible via `kubectl describe` for operational observability.

### Example ImageSync Resource

```yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: chainguard-base-images
  namespace: portage-system
spec:
  schedule: "0 */6 * * *"
  source:
    registry: cgr.dev/my-org
    authSecretRef:
      name: chainguard-pull-token
  destination:
    registry: 123456789012.dkr.ecr.us-gov-west-1.amazonaws.com
    auth:
      method: ecr
    repositoryPrefix: "chainguard"
  images:
    - name: go
      tags: ["latest", "1.22"]
    - name: node
      tags: ["latest", "22"]
    - name: python
      tags: ["latest", "3.12"]
```

### What This Produces

```
$ kubectl get imagesync -n portage-system
NAME                      SYNCED   FAILED   LAST SYNC              NEXT SYNC              AGE
chainguard-base-images    6/6      0        2026-02-24T10:00:00Z   2026-02-24T16:00:00Z   7d

$ kubectl describe imagesync chainguard-base-images -n portage-system
...
Events:
  Type    Reason        Age   Message
  ----    ------        ----  -------
  Normal  ImageSynced   30m   Synced cgr.dev/my-org/go:1.22 (digest: sha256:abc12345)
  Normal  ImageSkipped  29m   Image cgr.dev/my-org/node:22 already up-to-date (digest: sha256:def45678)
  Normal  SyncComplete  29m   Sync complete: 6 synced, 0 failed, 6 total
```

## Architecture

```
                         Kubernetes Cluster
  ┌───────────────────────────────────────────────────────┐
  │                                                       │
  │   ImageSync CRD          Portage Controller           │
  │   (desired state)  --->  - Watches ImageSync          │
  │                          - Authenticates to registries│
  │   Secrets          --->  - Compares digests           │
  │   (auth creds)           - Copies changed images      │
  │                          - Updates .status            │
  │                          - Emits Kubernetes Events    │
  └──────────────┬───────────────────┬────────────────────┘
                 │                   │
                 v                   v
          ┌────────────┐     ┌────────────┐
          │   Source   │     │   Dest     │
          │  Registry  │     │  Registry  │
          │            │     │            │
          │  cgr.dev   │     │    ECR     │
          │  Docker Hub│     │    GCR     │
          │  GHCR      │     │   Harbor   │
          │  Quay      │     │    etc.    │
          └────────────┘     └────────────┘
```

## Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Standard for the Kubernetes ecosystem; required by Kubebuilder |
| Scaffolding | Kubebuilder | Standard operator framework; generates CRDs, controllers, and RBAC |
| Image copy engine | `go-containerregistry` (crane) | Native Go library; no shelling out to external CLIs; efficient layer-level transfers |
| Registry scope | Any OCI-compliant registry | Pluggable `Authenticator` interface with implementations for Secrets, ECR (IRSA), and GCR (workload identity) |
| Digest comparison | `crane.Digest` (HTTP HEAD) | Skips unchanged images without downloading layers; bandwidth-efficient and fast |
| Scheduling | Cron-based, evaluated in the controller | Standard cron expressions and shorthands like `@every 6h` |
| Status reporting | `.status` subresource with conditions | Follows standard Kubernetes conventions; compatible with GitOps tooling health checks |

## Authentication

Portage supports multiple authentication strategies through a pluggable interface:

- **Kubernetes Secrets** (`kubernetes.io/dockerconfigjson`) -- Works with any OCI registry. Reference a Secret in the `ImageSync` spec for source or destination authentication.
- **Anonymous** -- For public registries like Docker Hub. No configuration needed; simply omit the auth fields.
- **ECR via IRSA** (planned) -- Uses IAM Roles for Service Accounts to obtain short-lived ECR tokens. No long-lived credentials stored in the cluster.
- **GCR via Workload Identity** (planned) -- Uses GCP workload identity federation for keyless authentication.

## Project Status

Portage is under active development. The core reconciliation loop, image copy engine, digest-based skip logic, per-image status reporting, and Kubernetes event emission are implemented and tested. Scheduling, cloud-native authentication (ECR/GCR), and platform filtering are in progress.

This project is not yet packaged for production deployment. See the [development phases](#development-phases) below for current progress.

## Development Phases

| Phase | Description | Status |
|-------|-------------|--------|
| 0 | Project scaffolding, CRD types, minimal reconciler | Complete |
| 1 | Single image copy with pluggable auth (Secret + Anonymous) | Complete |
| 2 | Digest comparison, per-image status, Kubernetes Events | Complete |
| 3 | Cron-based scheduling with `RequeueAfter` | Planned |
| 4 | ECR authentication (IRSA), destination repo creation | Planned |
| 5 | Multi-arch platform filtering | Planned |
| 6 | Prometheus metrics, Helm chart, leader election | Planned |

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
