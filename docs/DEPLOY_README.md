# Deploying Portage

This guide walks through deploying and testing the Portage image sync controller on a Kubernetes cluster with AWS ECR as the destination registry.

Two paths are covered: **Kind cluster** (local development) and **EKS with IRSA** (production-like).

---

## Prerequisites

- Go 1.25+
- Docker
- kubectl
- AWS CLI (authenticated)
- Kind (for local testing) or an EKS cluster

---

## Path A: Kind Cluster

### 1. Create the cluster and build the controller image

```bash
kind create cluster --name portage-test
make docker-build IMG=portage:dev
kind load docker-image portage:dev --name portage-test
```

### 2. Install CRDs and deploy the controller

```bash
make install                    # installs the ImageSync CRD
make deploy IMG=portage:dev     # deploys controller + RBAC into portage-system namespace
```

### 3. Verify the controller is running

```bash
kubectl get pods -n portage-system
# Should show: portage-controller-manager-xxxxx   Running

kubectl logs -n portage-system -l control-plane=controller-manager -f
```

### 4. Inject AWS credentials into the controller pod

Kind doesn't have IRSA, so inject credentials via environment variables:

```bash
kubectl set env deployment/portage-controller-manager \
  -n portage-system \
  AWS_ACCESS_KEY_ID="$(aws configure get aws_access_key_id)" \
  AWS_SECRET_ACCESS_KEY="$(aws configure get aws_secret_access_key)" \
  AWS_REGION=us-east-1
```

### 5. Create a source auth secret (optional)

For public registries like Docker Hub, no secret is needed. For private registries like Chainguard:

```bash
kubectl create secret docker-registry chainguard-pull-secret \
  --docker-server=cgr.dev \
  --docker-username=_json_key \
  --docker-password="$(cat key.json)"
```

### 6. Apply an ImageSync resource

```yaml
# my-imagesync.yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: alpine-to-ecr
  namespace: default
spec:
  schedule: "@every 1h"
  source:
    registry: docker.io/library
  destination:
    registry: <ACCOUNT_ID>.dkr.ecr.us-east-1.amazonaws.com
    auth:
      method: ecr
    repositoryPrefix: portage-test
  createDestinationRepos: true
  images:
    - name: alpine
      tags: ["latest", "3.21"]
```

```bash
kubectl apply -f my-imagesync.yaml
```

### 7. Watch the reconciliation

```bash
# Controller logs (shows the full reconcile loop)
kubectl logs -n portage-system -l control-plane=controller-manager -f

# Events (human-readable summary)
kubectl describe imagesync alpine-to-ecr
# Expected events:
#   RepoEnsured  - ECR repository "portage-test/alpine" exists or was created
#   ImageSynced  - Synced docker.io/library/alpine:latest → ECR (digest: sha256:...)
#   ImageSynced  - Synced docker.io/library/alpine:3.21 → ECR
#   SyncComplete - Sync complete: 2 synced, 0 failed, 2 total

# Full status
kubectl get imagesync alpine-to-ecr -o jsonpath='{.status}' | jq .
```

The status shows:
- `lastSyncTime` — when it last ran
- `nextSyncTime` — when it will run again
- `conditions` — `Ready=True/SyncSucceeded`
- `images[].tags[].sourceDigest` — the digest used for comparison
- `syncedImages`, `failedImages`, `totalImages` — summary counts

### 8. Verify in AWS

```bash
aws ecr describe-repositories --region us-east-1
# Should show: portage-test/alpine

aws ecr list-images --repository-name portage-test/alpine --region us-east-1
# Should show: latest, 3.21
```

### 9. Test sync-now (force immediate re-sync)

```bash
kubectl annotate imagesync alpine-to-ecr portager.portager.io/sync-now=true
kubectl describe imagesync alpine-to-ecr
# Events show: ImageSkipped - Image already up-to-date (digests match)
```

### 10. Cleanup

```bash
kubectl delete imagesync alpine-to-ecr
aws ecr delete-repository --repository-name portage-test/alpine --force --region us-east-1
make undeploy
make uninstall
kind delete cluster --name portage-test
```

---

## Path B: EKS with IRSA

On EKS, the controller picks up credentials automatically via IAM Roles for Service Accounts (IRSA) instead of injected environment variables.

### 1. Create an IAM policy

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
  --policy-name PortageECRPolicy \
  --policy-document file://policy.json
```

### 2. Create an IAM role with IRSA trust policy

```bash
eksctl create iamserviceaccount \
  --name portage-controller-manager \
  --namespace portage-system \
  --cluster <CLUSTER_NAME> \
  --attach-policy-arn arn:aws:iam::<ACCOUNT_ID>:policy/PortageECRPolicy \
  --approve
```

Or manually create the role with a trust policy referencing your cluster's OIDC provider and annotate the service account:

```bash
kubectl annotate serviceaccount portage-controller-manager \
  -n portage-system \
  eks.amazonaws.com/role-arn=arn:aws:iam::<ACCOUNT_ID>:role/portage-controller
```

### 3. Deploy and use

The rest of the steps (build, deploy, apply ImageSync) are identical to Path A, except you skip the `kubectl set env` step — the AWS SDK picks up credentials automatically from the OIDC token projected into the pod.

---

## How it works internally

The reconcile loop for `method: ecr` with `createDestinationRepos: true`:

```
 1. Fetch ImageSync CR from the API server
 2. Validate the cron schedule expression
 3. Check for sync-now annotation (remove if present, bypass schedule)
 4. Schedule gate: skip if nextSyncTime is in the future
 5. Build destination authenticator (ECR):
    a. ParseECRRegion("599...amazonaws.com") → "us-east-1"
    b. LoadDefaultConfig(region) — picks up IRSA, env vars, or ~/.aws
    c. Return ECRAuthenticator wrapping the ECR SDK client
 6. Authenticate:
    a. GetAuthorizationToken → base64-encoded "AWS:<password>"
    b. Decode and return as authn.Authenticator for go-containerregistry
 7. Create destination repos (if createDestinationRepos is true):
    a. For each unique image name (with repositoryPrefix if set):
    b. DescribeRepositories — check if it exists
    c. If RepositoryNotFoundException → CreateRepository (mutable tags)
    d. Emit "RepoEnsured" event
 8. For each image + tag:
    a. Get source digest (HEAD request, no layer download)
    b. Get destination digest
    c. If digests match → skip, emit "ImageSkipped"
    d. If different or missing → crane.Copy, emit "ImageSynced"
 9. Update status: conditions, per-image results, counts
10. Compute nextSyncTime from cron schedule, requeue with RequeueAfter
```

---

## Sample CRs

See `config/samples/` for ready-to-use examples:

- `portager_v1alpha1_imagesync.yaml` — Docker Hub to local registry (no auth)
- `portager_v1alpha1_imagesync_ecr.yaml` — Chainguard to ECR with IRSA and repo creation
