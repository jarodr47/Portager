# Architecture

## Layout

```
api/v1alpha1/                  CRD types (ImageSync). zz_generated.* is generated — do not edit.
cmd/main.go                    Manager entrypoint and the single composition root.
internal/controller/
  imagesync_controller.go      Reconcile loop, auth selection, status writes
  auth/                        Authenticator interface + implementations
    authenticator.go             interface, plus the shared registryHost helper
    anonymous.go                 authn.Anonymous
    secret.go                    dockerconfigjson Secret
    ecr.go                       AWS ECR via GetAuthorizationToken (IRSA)
    gar.go                       Google Artifact Registry via ADC / Workload Identity
  metrics/                     portage_* Prometheus metrics
  registry/                    ECR repository auto-creation
  schedule/                    Cron parsing (robfig/cron/v3)
  sync/copier.go               ImageCopier + staticKeychain (crane)
  tags/resolver.go             TagLister, SemverResolver (Masterminds/semver)
  verify/                      Pre-sync validation gates
    verifier.go                  Validator + gate interfaces
    cosign.go                    signature verification (key-based + keyless)
    vulnerability.go             SARIF severity gating
    sbom.go                      SBOM existence gate
config/                        Kustomize manifests. crd/bases and rbac/role.yaml are generated.
helm/portager/                 Helm chart. crds/ is a copy of config/crd/bases.
test/e2e/                      Kind + Ginkgo suite (build tag `e2e`)
```

## Image path mapping

```
Source:      {spec.source.registry}/{image.name}:{tag}
Destination: {spec.destination.registry}/{spec.destination.repositoryPrefix}/{image.name}:{tag}
```

`cgr.dev/my-org/go:1.22` → `123456789012.dkr.ecr.us-east-1.amazonaws.com/chainguard/go:1.22`.
An empty `repositoryPrefix` puts images directly under the destination root.

## Reconcile flow

```
1.  Fetch ImageSync (nil on NotFound)
2.  Validate the cron schedule
3.  sync-now annotation present → bypass the schedule
4.  generation != observedGeneration → sync immediately
5.  Not due per nextSyncTime → requeue (only when the spec is unchanged)
6.  Build source and destination authenticators
7.  createDestinationRepos + ECR → ensure repositories exist
8.  Per image: if semver is set, list source tags, filter, merge with explicit tags
9.  Per image+tag:
      a. GetDigest on source (HTTP HEAD, no layer download)
      b. GetDigest on destination (may legitimately fail if absent)
      c. digests match → skip, emit ImageSkipped
      d. validation configured → run the gates, emit ImageVerified or ValidationFailed
      e. otherwise → crane.Copy, emit ImageSynced or SyncFailed
10. Single atomic status write (conditions, per-image results, counts, observedGeneration)
11. Emit SyncComplete
12. Requeue for the next interval
```

## Design patterns

- **`GenerationChangedPredicate`** on `SetupWithManager` — reconcile only on spec changes, not
  status-only updates. Without it, status writes retrigger reconciles in a loop.
- **Single atomic status write.** All conditions are set once at the end of reconcile.
- **`staticKeychain`** (`sync/copier.go`) routes credentials by registry hostname during `crane.Copy`,
  so one copy call can authenticate to two different registries.
- **Pluggable auth.** `Authenticator` returns an `authn.Authenticator`. It carries credentials only —
  no transport — which is why custom TLS cannot ride on it (see issue #61).
- **Pluggable validation.** A nil `Validator` on the reconciler skips validation entirely.
- **Per-reconcile vs process-wide.** `ImageCopier` is constructed inside `Reconcile`, so per-request
  state on it is safe. The verifiers and `CraneTagLister` are built once in `cmd/main.go` and shared
  across reconciles — per-request state on those is a data race once `MaxConcurrentReconciles > 1`.

## Status

```
ImageSyncStatus
├── ObservedGeneration
├── LastSyncTime, NextSyncTime
├── Conditions []metav1.Condition        (Ready, Syncing)
├── Images []ImageSyncStatusImage
│   └── Tags []TagSyncStatus             (tag, synced, sourceDigest, lastSyncTime,
│                                         error, verified, validationError)
└── TotalImages, SyncedImages, FailedImages
```

## Events

`RepoEnsured`, `TagsResolved`, `TagResolutionFailed`, `ImageSynced`, `ImageSkipped`, `ImageVerified`,
`ValidationFailed`, `SyncFailed`, `SyncComplete`

## Metrics

All custom metrics use the `portage_` prefix and live in `internal/controller/metrics/metrics.go`.

| Metric | Type | Labels |
|---|---|---|
| `portage_sync_total` | Counter | name, namespace, status |
| `portage_sync_duration_seconds` | Histogram | name, namespace |
| `portage_images_copied_total` | Counter | |
| `portage_images_skipped_total` | Counter | |
| `portage_images_failed_total` | Counter | |
| `portage_images_verified_total` | Counter | name, namespace |
| `portage_images_validation_failed_total` | Counter | name, namespace, gate |
| `portage_image_info` | Gauge | |

## Outbound TLS

Every registry call builds its own `crane.Option` / `remote.Option` slice inline; there is no shared
transport abstraction. The only transport-level logic is a `localhost`/`127.0.0.1` check in
`sync/copier.go` and `tags/resolver.go` that adds `crane.Insecure`.

Custom CA trust is therefore applied at the **process** level via `SSL_CERT_DIR`/`SSL_CERT_FILE` from
the Helm chart, not per connection. That covers everything uniformly — registries, AWS STS/ECR, Google
token endpoints, Sigstore — with no code changes, which is why the feature shipped chart-only.

Note that `verify/cosign.go` does **not** set `co.RegistryClientOpts`, so cosign's registry lookups use
`http.DefaultTransport` and the default keychain rather than the configured authenticator (issue #64).
