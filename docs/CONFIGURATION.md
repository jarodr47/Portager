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
| `nodeSelector` | `{}` | Node selector for pod scheduling |
| `tolerations` | `[]` | Tolerations for pod scheduling |
| `affinity` | `{}` | Affinity rules for pod scheduling |
| `topologySpreadConstraints` | `[]` | Topology spread constraints for pod scheduling |
| `priorityClassName` | `""` | Priority class name for the controller pod |
| `podAnnotations` | `{}` | Annotations added to controller pod metadata |
| `podLabels` | `{}` | Labels added to controller pod metadata |
| `imagePullSecrets` | `[]` | Image pull secrets for the controller pod |
| `extraEnv` | `[]` | Additional environment variables for the controller container |
| `extraVolumes` | `[]` | Additional volumes for the controller pod |
| `extraVolumeMounts` | `[]` | Additional volume mounts for the controller container |
| `podDisruptionBudget.enabled` | `false` | Enable PodDisruptionBudget for the controller |
| `podDisruptionBudget.minAvailable` | `1` | Minimum available pods during disruption |
| `startupProbe.enabled` | `false` | Enable startup probe (httpGet /healthz:8081, 5min budget) |
| `terminationGracePeriodSeconds` | `10` | Termination grace period for the controller pod |
| `networkPolicy.enabled` | `false` | Enable NetworkPolicy for the controller pod |
| `networkPolicy.metrics.namespaceSelector` | `{kubernetes.io/metadata.name: monitoring}` | Labels to match namespaces allowed to scrape metrics |
| `networkPolicy.egress.registryCIDRs` | `[]` | Restrict registry egress to specific CIDRs (empty = allow all) |
| `networkPolicy.egress.apiServerCIDR` | `""` | Restrict API server egress to a specific CIDR (empty = allow all) |

---

## Authentication

Portager supports multiple authentication strategies through a pluggable interface:

| Method | Use Case | Configuration |
|---|---|---|
| **Anonymous (source)** | Public source registries (Docker Hub, Quay, GHCR public) | Omit `spec.source.authSecretRef` |
| **Anonymous (destination, explicit)** | Public/local registries with no auth | `spec.destination.auth.method: anonymous` |
| **Anonymous (destination, legacy)** | Backward-compatible anonymous auth | `spec.destination.auth.method: secret` with no `secretRef` |
| **Kubernetes Secret** | Any registry with username/password or token auth | `spec.source.authSecretRef` or `spec.destination.auth.secretRef` referencing a `kubernetes.io/dockerconfigjson` Secret |
| **ECR (IRSA / IAM)** | Amazon ECR | `spec.destination.auth.method: ecr` — uses the AWS credential chain (IRSA, env vars, instance profile) |

> **Note:** For source registries, anonymous auth is the default when `authSecretRef` is omitted. For destination registries, `auth.method` is required by the CRD. Use `method: anonymous` for unauthenticated destinations (recommended). `method: secret` without a `secretRef` still works for backward compatibility but `method: anonymous` is the preferred explicit approach.

### AWS Credential Strategies

For ECR destinations, Portager uses the standard AWS credential chain. How you provide credentials depends on your cluster type.

#### EKS with IRSA (recommended for production)

IRSA provides short-lived, automatically rotated credentials with no secrets stored in the cluster.

```bash
# 1. Create IAM policy (see Deploy Guide for the full policy JSON)
aws iam create-policy --policy-name PortagerECRPolicy --policy-document file://policy.json

# 2. Create IRSA service account
eksctl create iamserviceaccount \
  --name portager --namespace portager-system \
  --cluster my-cluster \
  --attach-policy-arn arn:aws:iam::123456789012:policy/PortagerECRPolicy \
  --approve

# 3. Install with the IRSA annotation
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portager-system --create-namespace \
  --set serviceAccount.create=false \
  --set serviceAccount.name=portager
```

Or let Helm create the ServiceAccount with the annotation:

```bash
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portager-system --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::123456789012:role/portager-ecr-role
```

#### Non-EKS clusters (Kind, minikube, self-managed, GKE, AKS)

**Option A: Inline credentials via Helm values**

```bash
helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portager-system --create-namespace \
  --set aws.credentials.enabled=true \
  --set aws.credentials.accessKeyId=AKIAIOSFODNN7EXAMPLE \
  --set aws.credentials.secretAccessKey=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --set aws.credentials.region=us-east-1
```

**Option B: Existing Kubernetes Secret**

```bash
kubectl create secret generic aws-creds -n portager-system \
  --from-literal=AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --from-literal=AWS_REGION=us-east-1

helm install portager oci://ghcr.io/jarodr47/portager/charts/portager \
  -n portager-system --create-namespace \
  --set aws.existingSecret=aws-creds
```

**Option C: Inject after install (useful for SSO/session tokens)**

```bash
helm install portager helm/portager/ -n portager-system --create-namespace

eval "$(aws configure export-credentials --format env)"
kubectl set env deployment/portager-controller-manager -n portager-system \
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
      method: ecr                  # "ecr", "secret", or "anonymous"
      secretRef:                   # Optional: omit for anonymous dest auth
        name: dest-creds
    repositoryPrefix: mirror       # Optional: images land under mirror/<name>
  createDestinationRepos: true     # Optional: auto-create ECR repos
  validation:                      # Optional: pre-sync validation gates
    cosign:
      enabled: true
      publicKey: "..."             # PEM-encoded cosign public key
    vulnerabilityGate:
      enabled: true
      maxSeverity: high            # Block on high + critical
      requireCveReport: true       # Block if no SARIF report found (default)
    sbomGate:
      enabled: true                # Require SBOM (SPDX or CycloneDX) attached
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

**Spec changes trigger immediate sync automatically.** When you modify an ImageSync resource (add images, change tags, update auth config, etc.), the controller detects the spec generation change and syncs immediately, even if the cron schedule isn't due yet. No annotation needed — this works naturally with GitOps tools like ArgoCD and Flux.

To force a re-sync when the spec hasn't changed (e.g., to pick up upstream tag changes early):

```bash
kubectl annotate imagesync <name> portager.portager.io/sync-now=true
```

The controller removes the annotation after processing.

---

## Pre-Sync Validation

Optional validation gates can be configured to verify source images before syncing. When enabled, images that fail validation are **not copied** and are reported as failures.

### Cosign Signature Verification

Verify that source images are signed with [cosign](https://github.com/sigstore/cosign) before syncing. Supports key-based and keyless (Fulcio) verification.

**Key-based verification:**

```yaml
spec:
  validation:
    cosign:
      enabled: true
      publicKey: |
        -----BEGIN PUBLIC KEY-----
        MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
        -----END PUBLIC KEY-----
```

**Keyless verification (Fulcio/Rekor):**

```yaml
spec:
  validation:
    cosign:
      enabled: true
      keylessIssuer: "https://token.actions.githubusercontent.com"
```

> **Note:** Keyless verification requires network access to Fulcio and Rekor transparency log services. It will not work in air-gapped environments — use key-based verification instead.

> **Note:** Keyless verification downloads TUF trust metadata and caches it on disk. The Helm chart handles this automatically (writable `/tmp` volume + `TUF_ROOT` env var). For Kustomize or manual deployments, ensure the controller pod has a writable directory and set the `TUF_ROOT` environment variable to point to it (e.g., `TUF_ROOT=/tmp/.sigstore`). Key-based verification does not require this.

### Vulnerability Gate

Block images that have vulnerability findings at or above a severity threshold. Portager reads **existing** SARIF-formatted scan reports attached as OCI attestations (referrers) to the source image. It does **not** execute vulnerability scans — your CI pipeline or registry must attach the report first.

**Prerequisite:** The source image must have a SARIF vulnerability report attached as an OCI referrer with artifact type `application/sarif+json`. Tools like [Trivy](https://aquasecurity.github.io/trivy/) can scan and attach reports:

```bash
# Scan image and attach SARIF report as an OCI referrer
trivy image --format sarif --output report.sarif myregistry/myimage:v1.0
oras attach myregistry/myimage:v1.0 --artifact-type application/sarif+json report.sarif
```

```yaml
spec:
  validation:
    vulnerabilityGate:
      enabled: true
      maxSeverity: high            # Block on high and critical findings (default: critical)
      requireCveReport: true       # Block sync if no scan report is found (default: true)
```

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | `false` | Activate vulnerability gate checking |
| `maxSeverity` | `string` | `critical` | Severity threshold: `critical`, `high`, `medium`, or `low` |
| `requireCveReport` | `bool` | `true` | When `true`, block sync if no SARIF vulnerability report is found attached to the source image. Set to `false` to allow images without reports. |

**Severity ordering:** `critical > high > medium > low`. Setting `maxSeverity: high` blocks findings rated high or critical.

**How severity is determined:** Portager resolves each SARIF finding's severity using the most specific data available:

1. **CVSS score** (from `rule.properties.security-severity`) — mapped as: critical (≥9.0), high (≥7.0), medium (≥4.0), low (<4.0)
2. **Rule default level** (from `rule.defaultConfiguration.level`) — mapped as: error→high, warning→medium, note→low
3. **Result level** (from `result.level`) — same mapping as above

When a finding exceeds the threshold, the error message lists the specific CVE IDs and their resolved severities, e.g.:

```
vulnerability gate: 2 finding(s) at or above high severity: CVE-2024-001 (critical), CVE-2024-002 (high)
```

This appears in the `ValidationFailed` event and in `.status.images[].tags[].validationError`.

**Behavior:**

| Scenario | Result |
|---|---|
| SARIF report found, all findings below threshold | Sync proceeds |
| SARIF report found, findings at or above threshold | Sync blocked, `ValidationFailed` event with CVE list |
| No SARIF report, `requireCveReport: true` (default) | Sync blocked |
| No SARIF report, `requireCveReport: false` | Sync proceeds |
| Non-SARIF referrer attached | Skipped (no error) |

### SBOM Gate

Require that a Software Bill of Materials (SBOM) is attached as an OCI referrer before allowing sync. Supports SPDX (`application/spdx+json`) and CycloneDX (`application/vnd.cyclonedx+json`) formats.

```yaml
spec:
  validation:
    sbomGate:
      enabled: true
```

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | `bool` | `false` | Require an SBOM to be attached to the source image |

When enabled, images without an SPDX or CycloneDX SBOM attached as an OCI referrer are blocked. This is a compliance gate — it verifies the SBOM exists, it does not inspect its contents.

**How to attach an SBOM:**

```bash
# Generate and attach an SPDX SBOM
trivy image --format spdx-json --output sbom.spdx.json myregistry/myimage:v1.0
oras attach myregistry/myimage:v1.0 --artifact-type application/spdx+json sbom.spdx.json
```

### Combined Example

```yaml
spec:
  validation:
    cosign:
      enabled: true
      publicKey: |
        -----BEGIN PUBLIC KEY-----
        MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
        -----END PUBLIC KEY-----
    vulnerabilityGate:
      enabled: true
      maxSeverity: high
      requireCveReport: true
    sbomGate:
      enabled: true
```

When multiple gates are enabled, they run in order: cosign → vulnerability → SBOM. If any gate fails, subsequent gates are skipped. The `ValidationFailed` event and `.status.images[].tags[].validationError` field indicate which gate failed and why.

### Events

| Event | Type | Description |
|---|---|---|
| `ImageVerified` | Normal | Image passed all validation gates |
| `ValidationFailed` | Warning | Image failed a validation gate (details in message) |

### Metrics

| Metric | Type | Labels |
|---|---|---|
| `portage_images_verified_total` | Counter | name, namespace |
| `portage_images_validation_failed_total` | Counter | name, namespace, gate |

---

## Network Policies

When `networkPolicy.enabled` is set to `true`, a Kubernetes `NetworkPolicy` is created that restricts traffic to and from the controller pod:

**Ingress (allowed):**
- TCP 8443 (metrics) from namespaces matching `networkPolicy.metrics.namespaceSelector` labels (default: `kubernetes.io/metadata.name: monitoring`)

**Egress (allowed):**
- UDP/TCP 53 to any (DNS resolution)
- TCP 443 to any (HTTPS to OCI registries), optionally restricted via `networkPolicy.egress.registryCIDRs`
- TCP 6443 to any (Kubernetes API server), optionally restricted via `networkPolicy.egress.apiServerCIDR`

**All other traffic is denied** when the policy is active.

> **Note:** Network policies require a CNI plugin that supports them (e.g., Calico, Cilium, Weave Net). Not all CNI plugins enforce NetworkPolicy rules. This feature is disabled by default for compatibility.

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
  -n portager-system --create-namespace \
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
helm install portager helm/portager/ -n portager-system --create-namespace \
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
