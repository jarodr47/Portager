# Status

**Version:** v0.5.0 (released 09 AUG 2026)

## Implemented

- CRD types, reconciler, full sync loop with digest-based skip
- Auth: Kubernetes Secret (`dockerconfigjson`), anonymous, ECR via IRSA, GAR via Workload Identity / ADC
- Cron scheduling with `RequeueAfter`, plus a `sync-now` annotation for on-demand runs
- Per-image status, Kubernetes Events, `portage_*` Prometheus metrics
- ECR repository auto-creation (ECR only; other registries create on first push)
- Semver tag filtering — wildcards, ranges, tilde, caret, with a `maxTags` limit
- Pre-sync validation gates: cosign signature verification (key-based and keyless), vulnerability
  severity gating from SARIF attestations, SBOM existence gate (SPDX + CycloneDX)
- Helm chart hardening: PDB, affinity/anti-affinity/topology spread, priority class, startup probe,
  termination grace period, `extraEnv`/`extraVolumes`/`extraVolumeMounts`
- NetworkPolicy support; explicit `anonymous` auth method
- Custom CA trust for private registries (`caBundle` Helm values) — mounts a PEM bundle from a Secret,
  ConfigMap or inline string and points Go's trust store at it via `SSL_CERT_DIR`/`SSL_CERT_FILE`.
  `append` unions with the public roots; `replace` trusts only the supplied CA. Applied at the process
  level, so it covers registry traffic, AWS STS/ECR, Google token endpoints and Sigstore uniformly.
- Supply chain: all GitHub Actions pinned to commit SHAs, cosign keyless signing on tagged releases,
  SPDX + CycloneDX SBOMs as OCI attestations, SLSA provenance via `provenance: mode=max`, Dependabot
  with grouped updates

## Not implemented

**Multi-arch platform filtering.** All platforms are copied today; `crane.Copy` moves whole manifest
lists transparently. There is no `platforms` field on the CRD. A design sketch exists in the
maintainer's local notes.

**Per-resource custom CA** (`tls.caProvider` on source/destination). The chart-level `caBundle` applies
cluster-wide and needs a pod restart to rotate. Tracked as issue #62 under epic #69.

**ACR (Azure) support.** Contributor PR #44 has been open since 01 APR 2026. A rebase with review fixes
was built on 09 AUG 2026 and **lost before being committed** — see the note at the bottom of this file.

**Stretch:** webhook triggers, cluster-scoped `ImageSyncPolicy` for governance, dry-run mode,
Slack/webhook notifications.

## Known issues

| Issue | Detail |
|---|---|
| #64 | `verify/cosign.go` never sets `co.RegistryClientOpts`, so cosign falls back to its default keychain and can only verify images in registries allowing anonymous manifest reads. |
| #65 | `config/manager/manager.yaml` lacks the writable `/tmp` emptyDir and `TUF_ROOT` the Helm chart sets, so keyless cosign verification does not work under `make deploy`. |
| #68 | ECR e2e tests exist behind `//go:build e2e_ecr` but there is no Makefile target to run them. GAR has `test-e2e-gar`. The ECR registry is also hardcoded in the test source. |
| — | Helm chart has no `aws.credentials.sessionToken`. For SSO/temporary creds, inject `AWS_SESSION_TOKEN` via `kubectl set env` after install. |
| — | ServiceMonitor ships `insecureSkipVerify: true`, an SC-8 finding. Roadmap item 7.3. |

## Open threads

- **Trivy is clean as of 12 OCT 2026** — 0 CRITICAL/HIGH in both the Debian base and the Go binary,
  down from 17 HIGH. Cleared on `chore/dependabot-oct` by the consolidated Dependabot batch plus three
  bumps past what Dependabot proposed. Note `trivy.yml` triggers only on push to main and PRs
  targeting main, so a green branch build does not mean Trivy ran.
- **Dependabot batch consolidated, not merged yet.** All 5 group PRs (#82 kubernetes, #83 go-modules,
  #85 sigstore, #86 actions, #88 aws-sdk) are applied as separate commits on `chore/dependabot-oct`,
  stacked on `chore/docs-restructure` (PR #89). Merging #89 first, then this, closes all five.

  Two things learned worth keeping:

  **No single group PR could ever have gone green.** Each bumps only its own group, so the HIGH
  findings in the other groups' modules remained and Trivy failed on all five. They had to land
  together.

  **Dependabot's pinned versions go stale.** The PRs were opened 09–16 SEP; by 12 OCT five HIGH
  findings had fixes newer than what they proposed (x/mod 0.41.0 vs 0.39.0, x/net 0.60.0 vs 0.58.0,
  grpc 1.83.2 — never proposed at all). Applying the batch verbatim is not the same as clearing
  Trivy; rescan after.
- **Epic #69** (custom CA / private trust) — layer 1 shipped in v0.5.0. Remaining: #61 transport
  package, #62 per-resource `tls.caProvider`, #63 threading it through the validation gates.
- **Tier 4 e2e coverage** is the recurring blocker. It is why the custom CA feature needed
  hand-verification, why #67 is parked, and why FIPS work cannot start.

## Lost work: ACR rebase, 09 AUG 2026

A full rebase of PR #44 was built and verified on branch `feat/acr-auth` — sovereign-cloud scope
derivation (`.azurecr.io` public, `.azurecr.us` US Government, China deliberately unsupported to match
ECR's rejection of AWS China), a 30s exchange timeout, ACR's error body surfaced into the returned
error, `httptest` wire-contract tests pinning the `/oauth2/exchange` request, an opt-in `e2e_acr` suite,
Helm workload-identity values, and docs in CONFIGURATION.md. It passed `make test`, Kind e2e, Trivy 0
HIGH and lint-parity.

**It was never committed and is gone.** The branch exists with zero commits beyond main. If ACR is
picked up again, the design decisions above are worth reproducing; the verified facts were that the
null-GUID username, the `/.default` scope form, and the `azure.workload.identity/use` pod label are all
correct per Microsoft Learn, and that `azcore`/`azidentity` are already indirect dependencies via
`sigstore/cosign`, so ACR adds almost no new supply-chain surface.
