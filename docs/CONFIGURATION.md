# Configuration

Complete reference for Portager's Helm values, authentication, ImageSync spec, scheduling, and metrics.

For deployment walkthroughs, see the [Deploy Guide](DEPLOY_README.md). For a project overview, see the [README](../README.md).

---

## Helm Values

| Parameter | Default | Description |
|---|---|---|
| `image.repository` | `ghcr.io/jarodr47/portager` | Controller image repository |
| `image.tag` | Chart `appVersion` | Controller image tag |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `replicaCount` | `1` | Number of controller replicas |
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `128Mi` | Memory limit |
| `resources.requests.cpu` | `10m` | CPU request |
| `resources.requests.memory` | `64Mi` | Memory request |
| `leaderElection.enabled` | `true` | Enable leader election (required for multi-replica) |
| `metrics.enabled` | `true` | Enable Prometheus metrics endpoint on `:8443` |
| `metrics.serviceMonitor.enabled` | `false` | Create a Prometheus `ServiceMonitor` (requires Prometheus Operator) |
| `serviceAccount.create` | `true` | Create a ServiceAccount |
| `serviceAccount.name` | `""` | Override ServiceAccount name (defaults to release fullname) |
| `serviceAccount.annotations` | `{}` | Annotations (e.g., `eks.amazonaws.com/role-arn` for IRSA) |
| `aws.credentials.enabled` | `false` | Inject AWS credentials as env vars (for non-EKS clusters) |
| `aws.credentials.accessKeyId` | `""` | AWS access key ID |
| `aws.credentials.secretAccessKey` | `""` | AWS secret access key |
| `aws.credentials.region` | `us-east-1` | AWS region |
| `aws.existingSecret` | `""` | Name of an existing Secret containing AWS credentials |

---

## Authentication

Portager supports multiple authentication strategies through a pluggable interface:

| Method | Use Case | Configuration |
|---|---|---|
| **Anonymous** | Public registries (Docker Hub, Quay, GHCR public) | Omit auth fields |
| **Kubernetes Secret** | Any registry with username/password or token auth | `spec.source.authSecretRef` or `spec.destination.auth.secretRef` referencing a `kubernetes.io/dockerconfigjson` Secret |
| **ECR (IRSA / IAM)** | Amazon ECR | `spec.destination.auth.method: ecr` — uses the AWS credential chain (IRSA, env vars, instance profile) |

### AWS Credential Strategies

For ECR destinations, Portager uses the standard AWS credential chain. How you provide credentials depends on your cluster type.

#### EKS with IRSA (recommended for production)

IRSA provides short-lived, automatically rotated credentials with no secrets stored in the cluster.

```bash
# 1. Create IAM policy (see Deploy Guide for the full policy JSON)
aws iam create-policy --policy-name PortagerECRPolicy --policy-document file://policy.json

# 2. Create IRSA service account
eksctl create iamserviceaccount \
  --name portager --namespace portage-system \
  --cluster my-cluster \
  --attach-policy-arn arn:aws:iam::123456789012:policy/PortagerECRPolicy \
  --approve

# 3. Install with the IRSA annotation
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portage-system --create-namespace \
  --set serviceAccount.create=false \
  --set serviceAccount.name=portager
```

Or let Helm create the ServiceAccount with the annotation:

```bash
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portage-system --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/portager-ecr-role
```

#### Non-EKS clusters (Kind, minikube, self-managed, GKE, AKS)

**Option A: Inline credentials via Helm values**

```bash
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portage-system --create-namespace \
  --set aws.credentials.enabled=true \
  --set aws.credentials.accessKeyId=AKIAIOSFODNN7EXAMPLE \
  --set aws.credentials.secretAccessKey=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --set aws.credentials.region=us-east-1
```

**Option B: Existing Kubernetes Secret**

```bash
kubectl create secret generic aws-creds -n portage-system \
  --from-literal=AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --from-literal=AWS_REGION=us-east-1

helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portage-system --create-namespace \
  --set aws.existingSecret=aws-creds
```

**Option C: Inject after install (useful for SSO/session tokens)**

```bash
helm install portager helm/portager/ -n portage-system --create-namespace

eval "$(aws configure export-credentials --format env)"
kubectl set env deployment/portager-controller-manager -n portage-system \
  AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
  AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
  AWS_SESSION_TOKEN="$AWS_SESSION_TOKEN" \
  AWS_REGION=us-east-1
```

---

## ImageSync Spec Reference

```yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: my-sync
spec:
  schedule: "0 */6 * * *"          # Cron expression or @every shorthand
  source:
    registry: cgr.dev/my-org       # Source registry
    authSecretRef:                  # Optional: for private source registries
      name: source-creds
      namespace: default           # Optional: defaults to ImageSync namespace
  destination:
    registry: 123456789012.dkr.ecr.us-east-1.amazonaws.com
    auth:
      method: ecr                  # "ecr" or "secret"
      secretRef:                   # Required when method is "secret"
        name: dest-creds
    repositoryPrefix: mirror       # Optional: images land under mirror/<name>
  createDestinationRepos: true     # Optional: auto-create ECR repos
  images:
    - name: go
      tags: ["latest", "1.22"]
    - name: python
      tags: ["latest", "3.12"]
```

### Schedule Examples

| Expression | Meaning |
|---|---|
| `@every 1h` | Every hour |
| `@every 6h` | Every 6 hours |
| `0 */6 * * *` | Every 6 hours on the hour |
| `0 2 * * *` | Daily at 2 AM |
| `0 0 * * 0` | Weekly on Sunday at midnight |

### On-Demand Sync

To trigger an immediate sync outside the cron schedule:

```bash
kubectl annotate imagesync <name> portager.portager.io/sync-now=true
```

The controller removes the annotation after processing.

---

## Metrics

Portager exposes custom Prometheus metrics on the controller-runtime metrics endpoint (`:8443/metrics`, HTTPS with authn/authz).

| Metric | Type | Labels | Description |
|---|---|---|---|
| `portage_sync_total` | Counter | `name`, `namespace`, `status` | Reconcile completions (`success` / `failure`) |
| `portage_sync_duration_seconds` | Histogram | `name`, `namespace` | Duration of each reconcile cycle |
| `portage_images_copied_total` | Counter | `name`, `namespace` | Images actually copied |
| `portage_images_skipped_total` | Counter | `name`, `namespace` | Images skipped (digest match) |
| `portage_images_failed_total` | Counter | `name`, `namespace` | Image copy failures |
| `portage_image_info` | Gauge | `name`, `namespace`, `synced`, `failed`, `total` | Current state snapshot per ImageSync |

Standard controller-runtime metrics (`controller_runtime_reconcile_total`, `workqueue_*`, etc.) are also available on the same endpoint.

### Enabling Prometheus Scraping

**With Prometheus Operator (ServiceMonitor):**

```bash
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portage-system --create-namespace \
  --set metrics.serviceMonitor.enabled=true
```

**With Kustomize:** The ServiceMonitor is included in the default kustomization.

---

## Development

### Prerequisites

- Go 1.25+
- Docker
- [Kind](https://kind.sigs.k8s.io/)

### Build and Test

```bash
make build          # Build the controller binary
make test           # Run unit and integration tests
make lint           # Run golangci-lint
make helm-lint      # Lint the Helm chart
make helm-template  # Render Helm templates locally
```

### Local Development Cycle

```bash
kind create cluster --name portager-dev
make docker-build IMG=portager:dev
kind load docker-image portager:dev --name portager-dev
helm install portager helm/portager/ -n portage-system --create-namespace \
  --set image.repository=portager --set image.tag=dev --set image.pullPolicy=Never
```

### Development Phases

| Phase | Description | Status |
|---|---|---|
| 0 | Project scaffolding, CRD types, minimal reconciler | Complete |
| 1 | Single image copy with pluggable auth (Secret + Anonymous) | Complete |
| 2 | Digest comparison, per-image status, Kubernetes Events | Complete |
| 3 | Cron-based scheduling with `RequeueAfter`, sync-now annotation | Complete |
| 4 | ECR authentication (IRSA), destination repo creation | Complete |
| 5 | Multi-arch platform filtering | Deferred |
| 6 | Prometheus metrics, Helm chart, leader election | Complete |
