<p align="center">
  <img src="portager-logo.svg" alt="Portager Logo" width="200">
</p>

<h1 align="center">Portager</h1>

<p align="center">
  <strong>Kubernetes operator for declarative registry-to-registry image sync</strong>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &bull;
  <a href="docs/CONFIGURATION.md">Configuration</a> &bull;
  <a href="docs/DEPLOY_README.md">Deploy Guide</a> &bull;
  <a href="docs/CONFIGURATION.md#metrics">Metrics</a>
</p>

---

Portager is a Kubernetes operator that declaratively syncs container images between OCI-compliant registries. Define an `ImageSync` custom resource specifying source images, a destination registry, a cron schedule, and authentication — the operator handles the rest.

The name comes from the act of carrying cargo between two bodies of water, which is exactly what this does: carrying container images between two registries.

## How It Works

You create an `ImageSync` CR. The operator compares source and destination digests on your cron schedule, copies only what's changed, and reports status back on the resource. No pipelines, no scripts — just a CRD and a controller.

```
                    ┌──────────────┐
                    │  ImageSync   │
                    │     CRD      │
                    └──────┬───────┘
                           │
                           ▼
  ┌─────────┐     ┌────────────────┐     ┌─────────┐
  │  Source │────▶│   Portager     │────▶│  Dest   │
  │ Registry│     │  Controller    │     │ Registry│
  └─────────┘     └────────────────┘     └─────────┘
   Docker Hub      Digest compare         ECR
   GHCR            Copy if changed        GCR
   Chainguard      Skip if matching       Harbor
   Quay            Update status          Nexus
```

## Why Portager

There is no CRD-native operator in the Kubernetes ecosystem for declarative registry-to-registry image relocation. The existing alternatives each fall short:

| Alternative | Limitation |
|---|---|
| **ECR pull-through cache** | AWS-only, no support for arbitrary private registries (e.g., Chainguard `cgr.dev`), unavailable in GovCloud |
| **dregsy** | Config-file driven, not CRD-driven — no `kubectl get imagesync` |
| **kube-image-keeper (kuik)** | Caches to an in-cluster registry only; does not push to external registries |
| **Flux image automation** | Watches for new tags and updates Git manifests; does not copy images |
| **CI/CD pipelines** | Couples relocation to CI availability; no Kubernetes-native status reporting |

Portager fills this gap with a Kubernetes-native, GitOps-friendly approach that works with any OCI-compliant registry.

## Features

- **Declarative** — Define image sync rules as Kubernetes custom resources
- **Digest-based skip** — Compares manifest digests via HTTP HEAD; skips unchanged images without downloading layers
- **Registry-agnostic** — Works with Docker Hub, GHCR, Quay, Chainguard, ECR, GCR, Harbor, Nexus, and any OCI-compliant registry
- **Pluggable auth** — Kubernetes Secrets (`dockerconfigjson`), ECR via IRSA, or anonymous for public registries
- **Cron scheduling** — Standard cron expressions, shorthands like `@every 6h`, and on-demand sync via annotation
- **Observable** — Per-image status on the resource, Kubernetes Events, and custom Prometheus metrics

## Installation

### Prerequisites

- Kubernetes 1.28+
- [Helm](https://helm.sh/) v3+

### Install with Helm

```bash
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  --version 0.2.1 -n portager-system --create-namespace
```

<details>
<summary>Install with Kustomize</summary>

```bash
git clone https://github.com/jarodr47/portager.git
cd portager
make install   # Install CRDs
make deploy    # Deploy controller + RBAC
```
</details>

### Verify

```bash
kubectl get pods -n portager-system
# NAME                                           READY   STATUS    AGE
# portager-controller-manager-xxxxxxxxxx-xxxxx   1/1     Running   30s
```

## Quick Start

```yaml
# base-images.yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: base-images
  namespace: default
spec:
  schedule: "@every 6h"
  source:
    registry: docker.io/library
  destination:
    registry: 123456789012.dkr.ecr.us-east-1.amazonaws.com
    auth:
      method: ecr
    repositoryPrefix: mirror
  createDestinationRepos: true
  images:
    - name: alpine
      tags: ["3.21", "latest"]
    - name: nginx
      tags: ["1.27", "latest"]
```

```bash
kubectl apply -f base-images.yaml
kubectl describe imagesync base-images
```

```
Events:
  Type    Reason        Message
  ----    ------        -------
  Normal  RepoEnsured   ECR repository "mirror/alpine" exists or was created
  Normal  ImageSynced   Synced docker.io/library/alpine:3.21 -> ECR (digest: sha256:c3f8e73f)
  Normal  SyncComplete  Sync complete: 4 synced, 0 failed, 4 total
```

For full deployment walkthroughs — including EKS with IRSA, non-EKS clusters, and private source registries — see the **[Deploy Guide](docs/DEPLOY_README.md)**.

For Helm values, ImageSync spec reference, auth strategies, and metrics, see **[Configuration](docs/CONFIGURATION.md)**.

## Uninstalling

```bash
kubectl delete imagesync --all -A
helm uninstall portager -n portager-system
kubectl delete crd imagesyncs.portager.portager.io
kubectl delete ns portager-system
```

<details>
<summary>Uninstall with Kustomize</summary>

```bash
kubectl delete imagesync --all -A
make undeploy
make uninstall
```
</details>

## Contributing

```bash
make build          # Build the controller binary
make test           # Run unit and integration tests
make lint           # Run golangci-lint
make helm-lint      # Lint the Helm chart
```

See [Configuration — Development](docs/CONFIGURATION.md#development) for the full local development setup.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.
