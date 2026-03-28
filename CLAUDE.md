# CLAUDE.md

## Project Overview

Portager is a Kubernetes operator that declaratively syncs container images between OCI-compliant registries. Users define `ImageSync` custom resources specifying source images, destination registries, cron schedules, and authentication — the operator handles the rest.

- **Domain:** `portager.io`
- **API Group:** `portager.portager.io/v1alpha1`
- **CRD:** `ImageSync` (namespaced)
- **Repository:** `github.com/jarodr47/portager`
- **License:** Apache 2.0

## Current Status

**Version:** v0.3.0

### Implemented (Phases 0-4, 6 + Tier 1 + CR.1)
- CRD types, reconciler, full sync loop
- Secret-based auth, anonymous auth, ECR auth (IRSA)
- Digest comparison, per-image status, Kubernetes Events
- Cron scheduling with RequeueAfter, sync-now annotation
- ECR repo auto-creation
- Prometheus metrics, Helm chart, leader election
- CI: unit tests, e2e tests, multi-arch build + push, Helm OCI publish, Trivy scanning
- Supply chain security: all GitHub Actions pinned to commit SHAs, cosign keyless image signing on tagged releases, SBOM generation (SPDX + CycloneDX) attached as OCI attestations, SLSA provenance via `provenance: mode=max`, Dependabot for automated dependency updates
- Pre-sync validation gates: cosign signature verification (key-based and keyless), vulnerability severity gating via OCI attestation SARIF reports, SBOM existence gate (SPDX + CycloneDX)
- Semver tag filtering: auto-discover tags matching semver constraints (wildcards, ranges, tilde, caret) with configurable maxTags limit

### Not Implemented

#### Phase 5: Multi-arch Platform Filtering

The `platforms` field is not yet in the CRD types. Currently all platforms are copied. The design:

Add `platforms` to both `ImageSyncSpec` (default for all images) and `ImageSpec` (per-image override):

```yaml
spec:
  platforms:                    # default for all images
    - os: linux
      architecture: amd64
    - os: linux
      architecture: arm64
  images:
    - name: go
      tags: ["1.22"]
      platforms: all            # override: copy entire manifest list
    - name: python
      tags: ["3.12"]
      platforms:                # override: only amd64
        - os: linux
          architecture: amd64
    - name: node
      tags: ["22"]             # inherits spec.platforms (amd64 + arm64)
```

**Platform resolution precedence:**
1. `image.platforms == "all"` → no filtering, copy entire manifest list as-is
2. `image.platforms == [list]` → use image-level platform list
3. `image.platforms` unset → fall back to `spec.platforms`
4. Both unset → copy all platforms (same as `"all"`)

**Implementation notes:**
- When filtering: fetch source manifest index, build a new index with only matching platforms, push filtered index + referenced manifests
- When source is a single manifest (not a list): validate it matches the requested platform, warn if not
- Report synced platforms in `.status.images[].tags[].platforms`
- Key library: `go-containerregistry` has `v1.ImageIndex` and `v1.Platform` types for manifest list manipulation

#### Phase 7 (Stretch Features)
- **Webhook triggers** — endpoint for registries to call on new image push, triggering immediate sync
- **ImageSyncPolicy** — cluster-scoped CRD for governance: org-wide defaults (destination, platforms, schedule) and policy controls (allow/deny registries, image name patterns, tag restrictions like blocking `latest`)
- **Dry-run mode** — `spec.dryRun: true` evaluates what would sync without copying
- **Notifications** — Slack/webhook alerts on sync failures

### Known Issues
- Helm chart doesn't support `aws.credentials.sessionToken`
- golangci-lint reports 6 warnings (ginkgolinter, goconst, gocyclo, modernize, staticcheck x2)

## Quick Reference

```bash
make build            # Build controller binary
make test             # Unit/integration tests (envtest)
make lint             # golangci-lint (v2, config in .golangci.yml)
make docker-build IMG=portager:dev  # Build container image
make install          # Install CRDs into cluster
make deploy IMG=...   # Deploy controller via kustomize
make helm-lint        # Lint Helm chart
make helm-template    # Render Helm templates locally
```

## Project Structure

```
├── api/v1alpha1/                  # CRD type definitions (ImageSync)
├── cmd/main.go                    # Controller entrypoint and wiring
├── internal/controller/
│   ├── imagesync_controller.go    # Main reconciliation logic
│   ├── auth/                      # Authentication strategies
│   │   ├── authenticator.go       #   Authenticator interface
│   │   ├── anonymous.go           #   Public registries (authn.Anonymous)
│   │   ├── secret.go              #   dockerconfigjson Secret-based auth
│   │   └── ecr.go                 #   AWS ECR via IRSA / GetAuthorizationToken
│   ├── metrics/                   # Prometheus metrics (portage_* custom metrics)
│   ├── registry/                  # Registry operations (ECR repo auto-creation)
│   ├── schedule/                  # Cron parsing via robfig/cron/v3
│   ├── sync/                      # Image copy via go-containerregistry (crane)
│   │   └── copier.go              #   ImageCopier with staticKeychain
│   ├── tags/                      # Semver tag filtering
│   │   └── resolver.go            #   TagLister, SemverResolver (Masterminds/semver)
│   └── verify/                    # Pre-sync validation gates
│       ├── verifier.go            #   Validator, CosignVerifier, VulnerabilityChecker, SbomChecker interfaces
│       ├── cosign.go              #   Cosign signature verification (key-based + keyless)
│       ├── vulnerability.go       #   SARIF-based vulnerability severity gating
│       └── sbom.go                #   SBOM existence gate (SPDX + CycloneDX)
├── config/                        # Kustomize manifests (CRDs, RBAC, manager)
├── helm/portager/                 # Helm chart (v0.3.0)
├── test/e2e/                      # E2E tests (Kind + Ginkgo)
├── docs/
│   ├── CONFIGURATION.md           # Helm values, auth strategies, spec reference
│   ├── DEPLOY_README.md           # Deployment walkthroughs (EKS, non-EKS, etc.)
│   └── spec.md                    # Original design spec (historical)
├── .github/dependabot.yml         # Automated dependency updates (actions, gomod, docker)
└── .github/workflows/
    ├── test.yml                   # Unit/integration CI
    ├── test-e2e.yml               # E2E CI (Kind cluster)
    ├── build-push.yml             # Build + push + sign + SBOM + provenance
    └── trivy.yml                  # Container vulnerability scanning (SARIF → GitHub Security)
```

## Key Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| Go | 1.25 | Language |
| controller-runtime | 0.23.1 | Kubernetes controller framework |
| go-containerregistry | 0.21.1 | OCI image operations (crane) |
| aws-sdk-go-v2 | latest | ECR auth + repo creation |
| robfig/cron/v3 | 3.0.1 | Cron expression parsing |
| Masterminds/semver/v3 | 3.4.0 | Semver constraint parsing and matching |
| sigstore/cosign/v2 | 2.6.2 | Cosign signature verification |
| Ginkgo v2 / Gomega | 2.27+ | Testing framework |
| Kubebuilder | 4.12.0 | Scaffolding (go.kubebuilder.io/v4) |

## Image Path Mapping

Source and destination references are constructed as:

```
Source:      {spec.source.registry}/{image.name}:{tag}
Destination: {spec.destination.registry}/{spec.destination.repositoryPrefix}/{image.name}:{tag}
```

Example: `cgr.dev/my-org/go:1.22` → `123456789012.dkr.ecr.us-east-1.amazonaws.com/chainguard/go:1.22`

If `repositoryPrefix` is empty, images go directly under the destination registry root.

## Architecture

### Reconcile Flow

```
Reconcile(ImageSync) →
  1. Fetch ImageSync (return nil if NotFound)
  2. Validate cron schedule via Scheduler
  3. Check sync-now annotation → bypass schedule if set
  4. Check if spec changed (generation != observedGeneration) → sync immediately
  5. Check if due based on nextSyncTime → requeue if not (only when spec unchanged)
  6. Build source/dest authenticators (anonymous, secret, or ECR)
  7. If createDestinationRepos + ECR → ensure repos exist
  8. For each image:
     - If semver set → list tags from source, filter by constraint, merge with explicit tags
  9. For each image+tag:
     a. GetDigest on source (HTTP HEAD, no layer download)
     b. GetDigest on destination (may fail if not pushed yet)
     c. If digests match → skip, emit ImageSkipped event
     d. If validation configured → run cosign/vulnerability gates, emit ImageVerified or ValidationFailed
     e. If different/missing → crane.Copy, emit ImageSynced or SyncFailed
 10. Update .status (conditions, per-image results, summary counts, observedGeneration)
 11. Emit SyncComplete event
 12. Requeue after next schedule interval
```

### Key Design Patterns

- **`GenerationChangedPredicate`** on SetupWithManager — only reconciles on spec changes, not status-only updates. Prevents re-reconciliation loops.
- **Single atomic status write** — all conditions set in one update at the end of reconcile.
- **`staticKeychain`** in sync/copier.go — routes credentials to the correct registry during crane.Copy (source vs destination).
- **Pluggable auth** — `Authenticator` interface (`auth/authenticator.go`) with implementations for anonymous, secret-based, and ECR.
- **ECR repo auto-creation** — `registry/ecr.go` calls DescribeRepositories + CreateRepository. Only ECR is supported for auto-creation; other registries create repos on first push.
- **Pluggable validation** — `Validator` struct (`verify/verifier.go`) with `CosignVerifier` and `VulnerabilityChecker` interfaces. When Validator is nil on the reconciler, validation is entirely skipped.

### CRD Status Types

```
ImageSyncStatus
├── ObservedGeneration
├── LastSyncTime, NextSyncTime
├── Conditions: []metav1.Condition (Ready, Syncing)
├── Images: []ImageSyncStatusImage
│   └── Tags: []TagSyncStatus (tag, synced, sourceDigest, lastSyncTime, error, verified, validationError)
├── TotalImages, SyncedImages, FailedImages
```

### Events Emitted

`RepoEnsured`, `TagsResolved`, `TagResolutionFailed`, `ImageSynced`, `ImageSkipped`, `ImageVerified`, `ValidationFailed`, `SyncFailed`, `SyncComplete`

### Prometheus Metrics

All custom metrics use the `portage_` prefix. Defined in `internal/controller/metrics/metrics.go`:

| Metric | Type |
|--------|------|
| `portage_sync_total` | Counter (name, namespace, status) |
| `portage_sync_duration_seconds` | Histogram (name, namespace) |
| `portage_images_copied_total` | Counter |
| `portage_images_skipped_total` | Counter |
| `portage_images_failed_total` | Counter |
| `portage_images_verified_total` | Counter (name, namespace) |
| `portage_images_validation_failed_total` | Counter (name, namespace, gate) |
| `portage_image_info` | Gauge |

## Testing

**Unit/integration tests** use Ginkgo/Gomega with envtest (embedded API server). Use `fake-registry.invalid` as the registry hostname in tests to avoid real network calls.

```bash
make test             # Runs envtest-based tests
```

**E2E tests** use a Kind cluster (cluster name: `portage`). Build tag: `//go:build e2e`.

```bash
make setup-test-e2e   # Create Kind cluster
make test-e2e         # Run e2e suite
make cleanup-test-e2e # Delete Kind cluster
```

**ECR e2e tests** have a separate build tag: `//go:build e2e_ecr`.

## CI/CD

All GitHub Actions are pinned to full commit SHAs (not floating tags) for supply chain security. Dependabot (`.github/dependabot.yml`) proposes weekly PRs for action, Go module, and Docker base image updates.

- **test.yml** — Runs `make test` on push/PR
- **test-e2e.yml** — Spins up Kind, runs `make test-e2e` on push/PR
- **build-push.yml** — Builds multi-arch image (amd64/arm64) and pushes to GHCR. On every push: generates SPDX + CycloneDX SBOMs and attaches them as OCI attestations via cosign. On `v*` tags: signs images with cosign (keyless via Fulcio + Rekor), uploads SBOMs to GitHub Release, and publishes the Helm chart to `oci://ghcr.io/jarodr47/portager/charts`. SLSA provenance is attached via `docker/build-push-action` with `provenance: mode=max`.
- **trivy.yml** — Builds image locally and runs Trivy vulnerability scanner. Uploads SARIF to GitHub Security tab and fails on CRITICAL/HIGH findings.

## Gotchas

- **macOS port 5000** — AirPlay Receiver uses port 5000. Local test registry uses port 5001 instead.
- **`go mod tidy`** — Always run after `go get`. Go get doesn't always resolve transitive dependencies.
- **Insecure registry detection** — `sync/copier.go` checks for localhost/127.0.0.1 prefixes and adds `crane.Insecure`.
- **ECR token expiry** — ECR tokens expire every 12 hours, but the controller fetches a fresh token on every reconcile, so this is a non-issue.
- **Session tokens** — The Helm chart's `aws.credentials` values don't include `sessionToken`. For SSO/temporary creds, inject `AWS_SESSION_TOKEN` via `kubectl set env` after install.
- **golangci-lint** — Uses v2 config format (`.golangci.yml` with `version: "2"`). The `logcheck` plugin is NOT available as a built-in; don't add it back.
- **ServiceMonitor CRD** — The `config/default/kustomization.yaml` has `- ../prometheus` commented out. Uncommenting it requires Prometheus Operator CRDs to be installed first or `make deploy` will fail.
