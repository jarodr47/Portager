# Deploying Portager with Azure Container Registry (ACR)

This guide covers deploying Portager to sync images into Azure Container Registry using AKS Workload Identity.

| Path | Cluster | Azure Auth | Best For |
|---|---|---|---|
| [A: AKS + Workload Identity](#path-a-aks-with-workload-identity) | AKS | Workload Identity (no secrets in cluster) | Production |
| [B: Kind + Local Dev](#path-b-kind-local-development) | Kind (local) | Service Principal env vars | Local development and testing |

---

## Prerequisites

- Kubernetes 1.28+
- [Helm](https://helm.sh/) v3+
- kubectl configured for your cluster
- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) (`az`)

---

## Path A: AKS with Workload Identity

AKS Workload Identity federates Kubernetes ServiceAccount tokens with Azure AD, providing short-lived, automatically rotated credentials with no secrets stored in the cluster.

### 1. Set up variables

```bash
export RESOURCE_GROUP="my-resource-group"
export CLUSTER_NAME="my-aks-cluster"
export ACR_NAME="myregistry"                    # ACR name (not FQDN)
export LOCATION="eastus"
export IDENTITY_NAME="portager-identity"
export NAMESPACE="portager-system"
export SERVICE_ACCOUNT_NAME="portager"
```

### 2. Enable Workload Identity on your AKS cluster

If not already enabled:

```bash
az aks update \
  --resource-group $RESOURCE_GROUP \
  --name $CLUSTER_NAME \
  --enable-oidc-issuer \
  --enable-workload-identity
```

### 3. Create a User-Assigned Managed Identity

```bash
az identity create \
  --name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP \
  --location $LOCATION

export IDENTITY_CLIENT_ID=$(az identity show \
  --name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP \
  --query clientId -o tsv)

export IDENTITY_OBJECT_ID=$(az identity show \
  --name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP \
  --query principalId -o tsv)
```

### 4. Grant AcrPush role to the managed identity

```bash
export ACR_ID=$(az acr show --name $ACR_NAME --query id -o tsv)

az role assignment create \
  --assignee-object-id $IDENTITY_OBJECT_ID \
  --assignee-principal-type ServicePrincipal \
  --role AcrPush \
  --scope $ACR_ID
```

> **Note:** `AcrPush` includes both push and pull permissions. If the source registry is also an ACR, grant `AcrPull` on that ACR as well.

### 5. Create the federated credential

```bash
export AKS_OIDC_ISSUER=$(az aks show \
  --resource-group $RESOURCE_GROUP \
  --name $CLUSTER_NAME \
  --query "oidcIssuerProfile.issuerUrl" -o tsv)

az identity federated-credential create \
  --name portager-federated \
  --identity-name $IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP \
  --issuer $AKS_OIDC_ISSUER \
  --subject system:serviceaccount:${NAMESPACE}:${SERVICE_ACCOUNT_NAME} \
  --audiences api://AzureADTokenExchange
```

### 6. Install Portager with Helm

```bash
helm install portager helm/portager/ \
  -n $NAMESPACE --create-namespace \
  --set azure.workloadIdentity.enabled=true \
  --set azure.workloadIdentity.clientId=$IDENTITY_CLIENT_ID
```

This configures:
- ServiceAccount annotation: `azure.workload.identity/client-id: <clientId>`
- Pod label: `azure.workload.identity/use: "true"`

The AKS Workload Identity webhook automatically injects `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_FEDERATED_TOKEN_FILE` into the pod.

### 7. Create a pull secret for the source registry

Portager pulls images from a private Nexus registry using a `dockerconfigjson` Secret:

```bash
kubectl create secret docker-registry nexus-pull-secret \
  --docker-server=nexus.example.com \
  --docker-username=myuser \
  --docker-password=mypassword
```

### 8. Apply an ImageSync resource

```yaml
# nexus-to-acr.yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: nexus-to-acr
  namespace: default
spec:
  schedule: "@every 1h"
  source:
    registry: nexus.example.com
    authSecretRef:
      name: nexus-pull-secret
  destination:
    registry: myregistry.azurecr.io
    auth:
      method: acr
    repositoryPrefix: mirror
  images:
    - name: alpine
      tags: ["latest", "3.21"]
    - name: nginx
      tags: ["latest", "1.27"]
```

```bash
kubectl apply -f nexus-to-acr.yaml
```

> **Note:** ACR automatically creates repositories on first push, so `createDestinationRepos` is not needed (that feature is ECR-only).

### 9. Watch the reconciliation

```bash
# Events
kubectl describe imagesync nexus-to-acr
# Events:
#   ImageSynced  - Synced nexus.example.com/alpine:latest -> myregistry.azurecr.io/mirror/alpine:latest
#   SyncComplete - Sync complete: 4 synced, 0 failed, 4 total

# Full status
kubectl get imagesync nexus-to-acr -o jsonpath='{.status}' | jq .
```

### 10. Verify in Azure

```bash
az acr repository list --name $ACR_NAME -o table
az acr repository show-tags --name $ACR_NAME --repository mirror/alpine -o table
```

### 11. Cleanup

```bash
kubectl delete imagesync --all -A
helm uninstall portager -n $NAMESPACE
kubectl delete crd imagesyncs.portager.portager.io
az identity federated-credential delete --name portager-federated --identity-name $IDENTITY_NAME --resource-group $RESOURCE_GROUP
az role assignment delete --assignee $IDENTITY_OBJECT_ID --role AcrPush --scope $ACR_ID
az identity delete --name $IDENTITY_NAME --resource-group $RESOURCE_GROUP
```

---

## Path B: Kind (Local Development)

### 1. Build the controller image

```bash
# Build the controller binary and Docker image
make docker-build IMG=portager:dev

# Create a Kind cluster and load the image
kind create cluster --name portager-test
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

### 3. Inject Azure credentials

Kind doesn't have Workload Identity, so inject service principal credentials via environment variables:

```bash
kubectl set env deployment/portager-controller-manager -n portager-system \
  AZURE_CLIENT_ID="<your-app-client-id>" \
  AZURE_TENANT_ID="<your-tenant-id>" \
  AZURE_CLIENT_SECRET="<your-client-secret>"
```

> The `azidentity.NewDefaultAzureCredential()` picks up these environment variables automatically.

### 4. Create a pull secret for the source registry and apply an ImageSync

Same as [Path A steps 7-8](#7-create-a-pull-secret-for-the-source-registry) — create the Nexus pull secret and apply the ImageSync CR.

### 5. Cleanup

```bash
kubectl delete imagesync --all
helm uninstall portager -n portager-system
kubectl delete crd imagesyncs.portager.portager.io
kind delete cluster --name portager-test
```

---

## Building the Docker Image

### Local build (single architecture)

```bash
make docker-build IMG=portager:dev
```

This builds a linux image for your current architecture using the multi-stage `Dockerfile`:
1. **Builder stage:** `golang:1.26` — compiles the controller binary with `CGO_ENABLED=0`
2. **Runtime stage:** `gcr.io/distroless/static:nonroot` — minimal image with just the binary

### Push to a registry

```bash
# Push to GHCR
make docker-build IMG=ghcr.io/kubebn/portager:dev
make docker-push IMG=ghcr.io/kubebn/portager:dev

# Push to ACR
az acr login --name myregistry
make docker-build IMG=myregistry.azurecr.io/portager:dev
make docker-push IMG=myregistry.azurecr.io/portager:dev
```

### Multi-architecture build (amd64 + arm64)

```bash
make docker-buildx IMG=ghcr.io/kubebn/portager:dev PLATFORMS=linux/amd64,linux/arm64
```

> The CI pipeline (`.github/workflows/build-push.yml`) automatically builds multi-arch images and pushes to GHCR on every commit.

---

## Helm Values for ACR

### Minimal (AKS + Workload Identity)

```yaml
# values-acr.yaml
azure:
  workloadIdentity:
    enabled: true
    clientId: "00000000-0000-0000-0000-000000000000"  # Your managed identity client ID
```

```bash
helm install portager helm/portager/ \
  -n portager-system --create-namespace \
  -f values-acr.yaml
```

### Full example with all options

```yaml
# values-acr-full.yaml
image:
  repository: ghcr.io/jarodr47/portager
  tag: ""  # defaults to Chart.appVersion
  pullPolicy: IfNotPresent

replicaCount: 1

resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

leaderElection:
  enabled: true

metrics:
  enabled: true
  serviceMonitor:
    enabled: false

serviceAccount:
  create: true
  name: ""
  annotations: {}

azure:
  workloadIdentity:
    enabled: true
    clientId: "00000000-0000-0000-0000-000000000000"
```

---

## ImageSync CR Examples for ACR

### Nexus to ACR

The most common use case: pull from a private Nexus registry and push to ACR.

```bash
# Create the Nexus pull secret
kubectl create secret docker-registry nexus-pull-secret \
  --docker-server=nexus.example.com \
  --docker-username=myuser \
  --docker-password=mypassword
```

```yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: nexus-to-acr
spec:
  schedule: "@every 6h"
  source:
    registry: nexus.example.com
    authSecretRef:
      name: nexus-pull-secret
  destination:
    registry: myregistry.azurecr.io
    auth:
      method: acr
    repositoryPrefix: mirror
  images:
    - name: alpine
      tags: ["latest", "3.21"]
    - name: nginx
      tags: ["latest", "1.27"]
    - name: python
      tags: ["3.12", "3.13"]
```

### Nexus to ACR (multiple Nexus repositories)

If your Nexus has images under different repository paths, create separate ImageSync resources or use `repositoryPrefix` to organize them in ACR:

```yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: nexus-base-images-to-acr
spec:
  schedule: "0 */6 * * *"
  source:
    registry: nexus.example.com/docker-hosted
    authSecretRef:
      name: nexus-pull-secret
  destination:
    registry: myregistry.azurecr.io
    auth:
      method: acr
    repositoryPrefix: base-images
  images:
    - name: go
      tags: ["latest", "1.22"]
    - name: node
      tags: ["22", "20"]
```

### Nexus to ACR (secret in a different namespace)

If the Nexus pull secret lives in a different namespace, specify it explicitly:

```yaml
apiVersion: portager.portager.io/v1alpha1
kind: ImageSync
metadata:
  name: nexus-to-acr-cross-ns
  namespace: default
spec:
  schedule: "@every 1h"
  source:
    registry: nexus.example.com
    authSecretRef:
      name: nexus-pull-secret
      namespace: shared-secrets
  destination:
    registry: myregistry.azurecr.io
    auth:
      method: acr
  images:
    - name: my-app
      tags: ["latest", "v1.0.0"]
```

---

## How ACR Authentication Works

The ACR auth flow when using `method: acr`:

```
1. azidentity.NewDefaultAzureCredential()
   - On AKS with Workload Identity: reads AZURE_CLIENT_ID, AZURE_TENANT_ID,
     AZURE_FEDERATED_TOKEN_FILE (injected by the AKS webhook)
   - On local dev: reads AZURE_CLIENT_ID, AZURE_TENANT_ID, AZURE_CLIENT_SECRET

2. Get AAD access token scoped to https://<registry>.azurecr.io/.default

3. Exchange AAD token for ACR refresh token:
   POST https://<registry>.azurecr.io/oauth2/exchange
   Body: grant_type=access_token&service=<registry>&access_token=<aad_token>

4. Use the refresh token as password with username 00000000-0000-0000-0000-000000000000
   for go-containerregistry operations (crane.Copy)
```

Unlike ECR, ACR automatically creates repositories on first push, so `createDestinationRepos` is not needed.
