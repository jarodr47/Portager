# Deploying Portager

This guide walks through deploying Portager on Kubernetes clusters. Three deployment paths are covered:

| Path | Cluster | AWS Auth | Best For |
|---|---|---|---|
| [A: Kind + Helm](#path-a-kind-cluster-with-helm) | Kind (local) | IAM credentials via env vars | Local development and testing |
| [B: EKS + Helm with IRSA](#path-b-eks-with-irsa) | EKS | IRSA (no secrets in cluster) | Production |
| [C: Non-EKS + Helm](#path-c-non-eks-clusters) | GKE, AKS, self-managed | IAM credentials via Helm values or Secret | Any non-EKS Kubernetes cluster pushing to ECR |

All paths use the Helm chart. For Kustomize-based deployment, see the [Kustomize](#alternative-kustomize-deployment) section at the bottom.

---

## Prerequisites

- Kubernetes 1.28+
- [Helm](https://helm.sh/) v3+
- kubectl configured for your cluster
- AWS CLI (only for ECR destinations)

---

## Path A: Kind Cluster with Helm

### 1. Create the cluster and build the controller image

```bash
kind create cluster --name portager-test
make docker-build IMG=portager:dev
kind load docker-image portager:dev --name portager-test
```

### 2. Install with Helm

```bash
helm install portager helm/portager/ \
  -n portager-system --create-namespace \
  --set image.repository=portager \
  --set image.tag=dev \
  --set image.pullPolicy=Never
```

### 3. Verify the controller is running

```bash
kubectl get pods -n portager-system
# NAME                                           READY   STATUS    AGE
# portager-controller-manager-xxxxxxxxxx-xxxxx   1/1     Running   30s

kubectl logs -n portager-system -l control-plane=controller-manager --tail=10
# Should show: "Successfully acquired lease", "Starting Controller"
```

### 4. Inject AWS credentials

Kind doesn't have IRSA, so inject credentials via environment variables:

```bash
# For AWS SSO / aws sso login:
eval "$(aws configure export-credentials --format env)"
kubectl set env deployment/portager-controller-manager -n portager-system \
  AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
  AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
  AWS_SESSION_TOKEN="$AWS_SESSION_TOKEN" \
  AWS_REGION=us-east-1

# For long-lived IAM user credentials:
kubectl set env deployment/portager-controller-manager -n portager-system \
  AWS_ACCESS_KEY_ID="$(aws configure get aws_access_key_id)" \
  AWS_SECRET_ACCESS_KEY="$(aws configure get aws_secret_access_key)" \
  AWS_REGION=us-east-1
```

### 5. Apply an ImageSync resource

```yaml
# docker-to-ecr.yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: docker-to-ecr
  namespace: default
spec:
  schedule: "@every 1h"
  source:
    registry: docker.io/library
  destination:
    registry: <ACCOUNT_ID>.dkr.ecr.<REGION>.amazonaws.com
    auth:
      method: ecr
    repositoryPrefix: mirror
  createDestinationRepos: true
  images:
    - name: alpine
      tags: ["latest", "3.21"]
    - name: busybox
      tags: ["latest"]
```

```bash
kubectl apply -f docker-to-ecr.yaml
```

### 6. Watch the reconciliation

```bash
# Events (human-readable summary)
kubectl describe imagesync docker-to-ecr
# Events:
#   RepoEnsured  - ECR repository "mirror/alpine" exists or was created
#   ImageSynced  - Synced docker.io/library/alpine:latest -> ECR (digest: sha256:...)
#   SyncComplete - Sync complete: 3 synced, 0 failed, 3 total

# Full status
kubectl get imagesync docker-to-ecr -o jsonpath='{.status}' | jq .
```

### 7. Verify in AWS

```bash
aws ecr describe-repositories --region <REGION>
aws ecr list-images --repository-name mirror/alpine --region <REGION>
```

### 8. Test sync-now (force immediate re-sync)

```bash
kubectl annotate imagesync docker-to-ecr portager.portager.io/sync-now=true
kubectl describe imagesync docker-to-ecr
# Events show: ImageSkipped - Image already up-to-date (digests match)
```

### 9. Cleanup

```bash
kubectl delete imagesync --all
aws ecr delete-repository --repository-name mirror/alpine --force --region <REGION>
aws ecr delete-repository --repository-name mirror/busybox --force --region <REGION>
helm uninstall portager -n portager-system
kubectl delete crd imagesyncs.portager.portager.io
kubectl delete ns portager-system
kind delete cluster --name portager-test
```

---

## Path B: EKS with IRSA

On EKS, the controller picks up credentials automatically via IAM Roles for Service Accounts (IRSA). No long-lived credentials stored in the cluster.

### 1. Create an IAM policy

Save this as `portager-ecr-policy.json`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ecr:GetAuthorizationToken"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "ecr:BatchCheckLayerAvailability",
        "ecr:GetDownloadUrlForLayer",
        "ecr:BatchGetImage",
        "ecr:PutImage",
        "ecr:InitiateLayerUpload",
        "ecr:UploadLayerPart",
        "ecr:CompleteLayerUpload",
        "ecr:DescribeRepositories",
        "ecr:CreateRepository"
      ],
      "Resource": "arn:aws:ecr:<REGION>:<ACCOUNT_ID>:repository/*"
    }
  ]
}
```

```bash
aws iam create-policy \
  --policy-name PortagerECRPolicy \
  --policy-document file://portager-ecr-policy.json
```

### 2. Create the IRSA service account

**Option A: Using eksctl (recommended)**

```bash
eksctl create iamserviceaccount \
  --name portager \
  --namespace portager-system \
  --cluster <CLUSTER_NAME> \
  --attach-policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/PortagerECRPolicy \
  --approve
```

Then install Helm using the existing service account:

```bash
helm install portager helm/portager/ -n portager-system --create-namespace \
  --set serviceAccount.create=false \
  --set serviceAccount.name=portager
```

**Option B: Let Helm create the ServiceAccount with IRSA annotation**

Create the IAM role manually with a trust policy for your cluster's OIDC provider, then:

```bash
helm install portager helm/portager/ -n portager-system --create-namespace \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::<ACCOUNT_ID>:role/portager-ecr-role
```

### 3. Apply ImageSync resources and use

The rest of the workflow (apply ImageSync, watch reconciliation, verify in AWS) is identical to [Path A steps 5-8](#5-apply-an-imagesync-resource). No `kubectl set env` step needed — the AWS SDK picks up credentials automatically from the OIDC token projected into the pod.

### 4. Cleanup

```bash
kubectl delete imagesync --all -A
helm uninstall portager -n portager-system
kubectl delete crd imagesyncs.portager.portager.io
eksctl delete iamserviceaccount --name portager --namespace portager-system --cluster <CLUSTER_NAME>
aws iam delete-policy --policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/PortagerECRPolicy
```

---

## Path C: Non-EKS Clusters

For GKE, AKS, self-managed, or any Kubernetes cluster that needs to push to ECR but doesn't have IRSA.

### Option 1: AWS credentials via Helm values

```bash
helm install portager helm/portager/ -n portager-system --create-namespace \
  --set aws.credentials.enabled=true \
  --set aws.credentials.accessKeyId=AKIAIOSFODNN7EXAMPLE \
  --set aws.credentials.secretAccessKey=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --set aws.credentials.region=us-east-1
```

### Option 2: Existing Kubernetes Secret

```bash
kubectl create namespace portager-system

kubectl create secret generic aws-creds -n portager-system \
  --from-literal=AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE \
  --from-literal=AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --from-literal=AWS_REGION=us-east-1

helm install portager helm/portager/ -n portager-system \
  --set aws.existingSecret=aws-creds
```

### No ECR? No AWS credentials needed

If your destination is not ECR (e.g., Harbor, Nexus, GitLab Registry), you don't need any AWS configuration. Use `method: secret` with a `dockerconfigjson` Secret.

For registries that don't require authentication (e.g., a local `registry:2`), use `method: secret` without a `secretRef` — the controller falls back to anonymous auth:

```yaml
destination:
  registry: 172.18.0.3:5000
  auth:
    method: secret          # no secretRef = anonymous
```

For registries with credentials:

```bash
kubectl create secret docker-registry dest-creds \
  --docker-server=registry.example.com \
  --docker-username=myuser \
  --docker-password=mypassword

helm install portager helm/portager/ -n portager-system --create-namespace
```

```yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: hub-to-harbor
spec:
  schedule: "@every 6h"
  source:
    registry: docker.io/library
  destination:
    registry: registry.example.com
    auth:
      method: secret
      secretRef:
        name: dest-creds
    repositoryPrefix: mirror
  images:
    - name: alpine
      tags: ["latest"]
```

---

## Enabling Prometheus Metrics

Portager exposes custom metrics on `:8443/metrics` (HTTPS). To enable automatic scraping with the Prometheus Operator:

```bash
helm install portager helm/portager/ -n portager-system --create-namespace \
  --set metrics.serviceMonitor.enabled=true
```

This creates a `ServiceMonitor` that Prometheus will discover automatically. See [Configuration — Metrics](CONFIGURATION.md#metrics) for the full list of `portage_*` metrics.

---

## Private Source Registries

For private source registries (e.g., Chainguard `cgr.dev`), create a pull secret and reference it in the ImageSync:

```bash
kubectl create secret docker-registry chainguard-pull-secret \
  --docker-server=cgr.dev \
  --docker-username=_json_key \
  --docker-password="$(cat key.json)"
```

```yaml
spec:
  source:
    registry: cgr.dev/my-org
    authSecretRef:
      name: chainguard-pull-secret
```

---

## How It Works Internally

The reconcile loop for an ImageSync with `method: ecr` and `createDestinationRepos: true`:

```
 1. Fetch ImageSync CR from the API server
 2. Validate the cron schedule expression
 3. Check for sync-now annotation (remove if present, bypass schedule)
 4. Schedule gate: skip if nextSyncTime is in the future
 5. Build destination authenticator (ECR):
    a. ParseECRRegion("599...amazonaws.com") -> "us-east-1"
    b. LoadDefaultConfig(region) — picks up IRSA, env vars, or ~/.aws
    c. Return ECRAuthenticator wrapping the ECR SDK client
 6. Authenticate:
    a. GetAuthorizationToken -> base64-encoded "AWS:<password>"
    b. Decode and return as authn.Authenticator for go-containerregistry
 7. Create destination repos (if createDestinationRepos is true):
    a. For each unique image name (with repositoryPrefix if set):
    b. DescribeRepositories — check if it exists
    c. If RepositoryNotFoundException -> CreateRepository (mutable tags)
    d. Emit "RepoEnsured" event
 8. For each image + tag:
    a. Get source digest (HEAD request, no layer download)
    b. Get destination digest
    c. If digests match -> skip, emit "ImageSkipped"
    d. If different or missing -> crane.Copy, emit "ImageSynced"
 9. Update status: conditions, per-image results, counts
10. Record Prometheus metrics (sync_total, duration, copied/skipped/failed)
11. Compute nextSyncTime from cron schedule, requeue with RequeueAfter
```

The status on each ImageSync shows:
- `lastSyncTime` / `nextSyncTime` — when it last ran and will run again
- `conditions` — `Ready=True/SyncSucceeded` or `Ready=False/SyncFailed`
- `images[].tags[].sourceDigest` — the digest used for comparison
- `syncedImages`, `failedImages`, `totalImages` — summary counts

---

## Sample CRs

See `config/samples/` for ready-to-use examples:

- `portager_v1alpha1_imagesync.yaml` — Docker Hub to local registry (no auth)
- `portager_v1alpha1_imagesync_ecr.yaml` — Chainguard to ECR with IRSA and repo creation

---

## Alternative: Kustomize Deployment

If you prefer Kustomize over Helm:

```bash
# Build the controller image
make docker-build IMG=<your-registry>/portager:<tag>
make docker-push IMG=<your-registry>/portager:<tag>

# Install CRDs
make install

# Deploy controller
make deploy IMG=<your-registry>/portager:<tag>

# Undeploy
make undeploy
make uninstall
```

The Kustomize deployment includes the Prometheus ServiceMonitor by default. AWS credentials must be injected manually via `kubectl set env` (see [Path A step 4](#4-inject-aws-credentials)).
