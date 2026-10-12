# Development

## Commands

```bash
make build                           # controller binary
make test                            # unit + integration (envtest)
make manifests generate              # regenerate CRDs, RBAC, deepcopy — run after changing markers
make docker-build IMG=portager:dev   # container image
make install                         # CRDs into the current cluster
make deploy IMG=...                  # controller via kustomize
make helm-lint                       # lint the chart
make helm-template                   # render the chart locally
```

## Toolchain

| Dependency | Version | Notes |
|---|---|---|
| Go | 1.26 directive, `golang:1.26` builder | local toolchain may be newer; see the lint note below |
| controller-runtime | 0.24.1 | |
| k8s.io api / apimachinery / client-go | 0.36.3 | a minor bump here can raise the `go` directive |
| go-containerregistry | 0.21.9 | crane, the copy path |
| sigstore/cosign/v2 | 2.6.5 | |
| Masterminds/semver/v3 | 3.5.0 | |
| Ginkgo / Gomega | 2.32+ / 1.40+ | |
| Kubebuilder | 4.12.0 | scaffolding, `go.kubebuilder.io/v4` |

Verify with `go list -m -f '{{.Version}}' <module>` rather than trusting this table — it has drifted
before.

## Lint

Baseline is **17 findings**: ginkgolinter 1, goconst 3, gocyclo 1, modernize 8, staticcheck 4. All are
cosmetic or deprecation notices; none are functional. Lint is **not** wired into any CI workflow, so
nothing is gated on it — but a change should not add to the count.

- The 8 modernize hits are `new(expr)` suggestions in test helpers, which appeared when the `go`
  directive moved to 1.26.
- Two staticcheck entries are dependency-driven deprecations: `scheme.Builder` (controller-runtime 0.24)
  and `fulcioroots`, which upstream redirects to `sigstore-go/pkg/tuf`.

**The version is pinned in two places** and they must move together: `GOLANGCI_LINT_VERSION` in the
Makefile and `version:` in `.custom-gcl.yml`. The second exists because the build adds the `logcheck`
module plugin, producing a custom binary.

A Go toolchain bump can break lint without changing either file. v2.8.0 against Go 1.27 failed with
`export data version 4 is greater than maximum supported version 2`, and rebuilding did not help
because the pinned version itself predated the toolchain. The Makefile's version check still saw a
match, so it never rebuilt. If lint panics after a Go upgrade, bump both version strings — go1.27
support landed in golangci-lint v2.13.0.

`goconst` has `ignore-tests: true` in `.golangci.yml`. Without it, table-driven tests contribute ~32
findings on their own from repeating the same literals by design.

## Testing

**Unit and integration** use Ginkgo/Gomega with envtest (an embedded API server). Use
`fake-registry.invalid` as the registry hostname in tests to avoid real network calls.

```bash
make test
```

**E2E** uses Kind. Build tag `e2e`. The cluster is named **`portage-test-e2e`** (`KIND_CLUSTER` in the
Makefile), not `portage`.

```bash
make setup-test-e2e      # create the cluster
make test-e2e            # run, then tear the cluster down
make cleanup-test-e2e    # delete the cluster
```

**The e2e suite is two specs:** the manager starts, and the metrics endpoint serves. It never syncs an
image and never enters `verify/`. It is a real smoke test for startup-level breakage — a bad
otel/grpc/controller-runtime bump would fail it — but it cannot catch a regression in the copy path.

**Cloud-provider suites** are opt-in behind build tags and never run in CI:

```bash
GAR_REGISTRY=us-central1-docker.pkg.dev/proj/repo make test-e2e-gar
GAR_REGISTRY=... make cleanup-e2e-gar
```

ECR has `//go:build e2e_ecr` but no Makefile target, and hardcodes the registry in the test source —
issue #68.

## Verifying a change to the sync path

Because e2e cannot reach `sync/`, `tags/` or `verify/`, changes there need hand-verification. The
approach that has worked: run a local registry, then drive the real types with a temporary test file
behind a throwaway build tag, and delete it afterwards.

```bash
docker run -d --name tmp-registry -p 5001:5000 registry:2
```

Exercise `ImageCopier.Copy` (assert the digest is preserved), a manifest-list copy via
`random.Index` + `remote.WriteIndex`, `SemverResolver.ResolveTags` against live tags, and for the
sigstore stack `fulcioroots.Get()` — that call is what actually drives go-tuf and sigstore-go.

Port 5000 is taken by AirPlay Receiver on macOS; use 5001.

## CI

| Workflow | Trigger | Does |
|---|---|---|
| `test.yml` | push, PR | `go mod tidy` then `make test`, then **fails if git is dirty** |
| `test-e2e.yml` | push, PR | installs Kind, `make test-e2e` |
| `build-push.yml` | push to main, `v*` tags | multi-arch build to GHCR, SBOMs as OCI attestations. **On tags only:** cosign signing, SBOM upload to the Release, Helm chart publish |
| `trivy.yml` | push, PR | builds locally, scans, uploads SARIF, **fails on CRITICAL/HIGH** |

Two things this implies:

**Merging to main does not cut a release.** Signing, the GitHub Release and the chart publish are all
gated on `startsWith(github.ref, 'refs/tags/v')`. A release happens when a `v*` tag is pushed.

**`go mod tidy` must be a no-op.** `test.yml` runs tidy and a later step fails on any diff. Local Go is
often newer than the directive, so verify under the CI toolchain before handing over:

```bash
GOTOOLCHAIN=go1.26.0 go mod tidy && git diff --exit-code go.mod go.sum
```

This has caught real CI-breaking drift twice, including `go get` leaving a module tagged `// indirect`
when the code had started importing it directly.

## Platform gotchas

- **macOS port 5000** — AirPlay Receiver. Local registries use 5001.
- **BSD vs GNU sed** — `sed -i ''` for in-place on macOS, and BSD sed does **not** support `\b` word
  boundaries. A `sed` using `\b` silently does nothing. CI runs on Linux where both differ.
- **Custom CA cannot be tested on macOS** — Go's darwin root loader ignores `SSL_CERT_DIR` and
  `SSL_CERT_FILE` entirely. Verify `caBundle` behavior in a Linux container or in-cluster.
- **`go mod tidy` after every `go get`** — `go get` does not always resolve transitives, and does not
  fix `// indirect` markers.
- **ServiceMonitor CRD** — `config/default/kustomization.yaml` has `- ../prometheus` commented out.
  Uncommenting requires Prometheus Operator CRDs installed first, or `make deploy` fails.
- **Dependabot PR cap** — `open-pull-requests-limit` is 10 for gomod and github-actions. The default of
  5 jammed the queue for four months, silently blocking every newer update from being proposed.
