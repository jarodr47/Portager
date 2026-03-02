# Portager Code Walkthrough

A guided trace through the entire codebase, from binary startup to a complete image sync. Designed for someone who knows Kubernetes but is new to Go and operator development.

Use this with Claude (the app): paste the relevant file contents for each section and ask it to explain what's happening line by line.

---

## How to Use This Guide

1. Open each file listed below in your editor
2. Paste the file contents into Claude (the app) along with the prompt for that section
3. Ask follow-up questions as needed before moving to the next section
4. Follow the sections in order — they trace the execution path from startup to a full sync cycle

---

## Part 1: The Binary — How It Starts

### File: `Dockerfile`

**Prompt:**
> I'm learning Go and Kubernetes operator development. This is the Dockerfile for a Kubernetes operator called Portager. Explain what each stage does, what "distroless" means, why CGO is disabled, and why the binary is built with TARGETOS/TARGETARCH args. What does the final container actually contain?

**What you'll learn:** Multi-stage Docker builds, static Go binaries, distroless images, why operators are tiny containers.

---

### File: `cmd/main.go`

This is the entrypoint — the `main()` function that starts everything.

**Prompt:**
> This is the main.go entrypoint for a Kubernetes operator built with controller-runtime (Kubebuilder). Walk me through it section by section:
> 1. What does the `init()` function do with `scheme`? Why do we register schemes?
> 2. What are all the flags being parsed and what do they control?
> 3. What is the "Manager" and what does `ctrl.NewManager()` set up?
> 4. What is leader election and why does an operator need it?
> 5. How is the ImageSyncReconciler created and registered with the manager?
> 6. What happens when `mgr.Start()` is called at the end?
>
> Explain Go concepts as they come up (structs, interfaces, error handling patterns, the `:=` operator, etc.).

**What you'll learn:** How a controller-runtime operator boots up, the Manager pattern, scheme registration, leader election, health probes.

---

## Part 2: The CRD — What Users Define

### File: `api/v1alpha1/imagesync_types.go`

**Prompt:**
> This file defines the Kubernetes CRD types for an `ImageSync` custom resource. Walk me through every struct and field:
> 1. How do Go structs map to Kubernetes YAML fields?
> 2. What do the `json:` tags do? What about `omitempty`?
> 3. What are the `+kubebuilder:` comment directives? (validation, subresource, etc.)
> 4. How does the Spec vs Status pattern work in Kubernetes?
> 5. What is `metav1.Condition` and why is it the standard for status reporting?
> 6. What does `SchemeBuilder.Register()` at the bottom do?
>
> Show me what an applied YAML would look like for these types.

**What you'll learn:** CRD type definitions, Go struct tags, kubebuilder markers, the spec/status contract, Kubernetes conditions pattern.

---

## Part 3: The Reconciler — The Brain

### File: `internal/controller/imagesync_controller.go`

This is the biggest and most important file. Break it into three prompts.

**Prompt 1 — The Reconcile method (the main loop):**
> This is the main reconcile loop for a Kubernetes operator. It runs every time an ImageSync resource changes or a scheduled sync is due. Walk me through the Reconcile method step by step:
> 1. How does it fetch the resource? What happens if it's been deleted?
> 2. How does schedule validation work?
> 3. How does the sync-now annotation bypass the schedule?
> 4. How does the schedule check decide whether to sync or requeue?
> 5. How are authenticators built and used?
> 6. Walk me through the image copy loop — digest comparison, skip logic, copy, error handling
> 7. How is status updated at the end?
> 8. How does RequeueAfter work for the next scheduled sync?
>
> Explain Go patterns: error handling with `if err != nil`, the `switch` statement, `time.Since()`, `fmt.Sprintf()`, etc.

**Prompt 2 — The helper methods:**
> Now explain the helper methods in the same file:
> 1. `buildSourceAuth()` — how does it decide between anonymous and secret-based auth?
> 2. `buildDestAuth()` — how does the switch between secret/ecr/anonymous work?
> 3. `buildDestRef()` — how are destination image references constructed?
> 4. `truncateDigest()` — what's this for?
> 5. `updateStatusWithError()` — how does error status reporting work?
>
> For `buildDestAuth`, trace the ECR path: how does `awsconfig.LoadDefaultConfig` lead to IRSA credentials?

**Prompt 3 — SetupWithManager and predicates:**
> Explain the SetupWithManager method and the predicates:
> 1. What does `ctrl.NewControllerManagedBy(mgr).For(&ImageSync{})` do?
> 2. What is `GenerationChangedPredicate` and why is it critical? What would happen without it?
> 3. What is the custom `syncNowAnnotationPredicate`? Why does it return false on Create and true only on specific Updates?
> 4. How does `predicate.Or()` compose these two predicates?
>
> This is key to understanding when the reconcile loop fires and when it doesn't.

**What you'll learn:** The full reconcile pattern, error handling, auth flows, status management, controller predicates, requeue mechanics.

---

## Part 4: Authentication — How It Talks to Registries

### Files (paste all four together):
- `internal/controller/auth/authenticator.go`
- `internal/controller/auth/anonymous.go`
- `internal/controller/auth/secret.go`
- `internal/controller/auth/ecr.go`

**Prompt:**
> These four files implement pluggable authentication for a Kubernetes operator that copies container images between registries. Walk me through:
> 1. The `Authenticator` interface — what does it define? How is it used as a contract?
> 2. `AnonymousAuthenticator` — the simplest implementation. How does Go implement interfaces implicitly?
> 3. `SecretAuthenticator` — how does it read a Kubernetes Secret, parse Docker config JSON, and extract credentials for a specific registry?
> 4. `ECRAuthenticator` — how does it call the AWS ECR API to get a temporary Docker login? What is the token format?
> 5. `ParseECRRegion` — how does the regex extract the AWS region from an ECR hostname?
>
> Explain Go interfaces, the `authn.Authenticator` type from go-containerregistry, and how the controller picks which implementation to use.

**What you'll learn:** Go interfaces (implicit implementation), the strategy pattern, Kubernetes Secret reading, AWS ECR auth flow, Docker config JSON format.

---

## Part 5: Image Copying — The Actual Work

### File: `internal/controller/sync/copier.go`

**Prompt:**
> This file handles the actual OCI image copying between registries using Google's go-containerregistry (crane) library. Explain:
> 1. What does `crane.Copy()` do under the hood? (manifest fetching, layer copying, etc.)
> 2. What does `crane.Digest()` do? Why is it an HTTP HEAD request?
> 3. What is the `staticKeychain` type? Why does crane need a keychain, and how does this one route credentials to the right registry?
> 4. How does `isInsecureRegistry()` work and why is it needed?
> 5. What is the `authn.Keychain` interface and how does `Resolve()` work?
>
> This is where the actual network calls happen — explain what goes over the wire.

**What you'll learn:** OCI registry protocol basics, crane library, keychain pattern, digest comparison as an optimization, HTTP HEAD vs GET for manifests.

---

## Part 6: Scheduling — When Syncs Happen

### File: `internal/controller/schedule/cron.go`

**Prompt:**
> This file handles cron-based scheduling for a Kubernetes operator. It's small but important. Explain:
> 1. What is `robfig/cron/v3` and how does it parse cron expressions?
> 2. Why is the parser configured with `Minute | Hour | Dom | Month | Dow | Descriptor`? What does each flag enable?
> 3. How does `NextSyncTime()` work? Why does it take an `after` parameter instead of using `time.Now()` directly?
> 4. How does `Validate()` differ from `NextSyncTime()`?
> 5. How does this connect back to the reconciler's RequeueAfter mechanism?
>
> Explain how `@every 6h` and `0 */6 * * *` both work.

**What you'll learn:** Cron expression parsing, deterministic time functions for testability, how controller-runtime's RequeueAfter creates a scheduling loop.

---

## Part 7: ECR Repository Management

### File: `internal/controller/registry/ecr.go`

**Prompt:**
> This file auto-creates ECR repositories before pushing images. Explain:
> 1. The `ECRRepoClient` interface — why define a subset of the ECR API as an interface?
> 2. How does `EnsureRepositoryExists` implement the "check then create" pattern?
> 3. How does it detect a `RepositoryNotFoundException` using the AWS SDK v2 error types?
> 4. Why is `ImageTagMutability` set to `MUTABLE`?
> 5. How is this called from the reconciler? (reference the reconcile loop from Part 3)

**What you'll learn:** AWS SDK v2 patterns in Go, error type assertions, idempotent "ensure exists" pattern, interface-based testing.

---

## Part 8: Prometheus Metrics

### File: `internal/controller/metrics/metrics.go`

**Prompt:**
> This file defines custom Prometheus metrics for a Kubernetes operator. Explain:
> 1. What is `promauto` and how does it auto-register metrics?
> 2. The difference between Counter, Histogram, and Gauge — when to use each
> 3. What labels are on each metric and why?
> 4. How are the histogram buckets chosen for SyncDuration?
> 5. How does the ImageInfo gauge work as a "label carrier" (always set to 1.0)?
> 6. Where in the reconciler is each metric recorded? (reference back to Part 3)

**What you'll learn:** Prometheus metric types, labeling strategies, how controller-runtime exposes /metrics, how custom metrics integrate with the reconcile loop.

---

## Part 9: Testing

### Files:
- `internal/controller/suite_test.go`
- `internal/controller/imagesync_controller_test.go`

**Prompt:**
> These files test the Kubernetes operator using Ginkgo/Gomega and envtest. Explain:
> 1. What is envtest? How does it provide a Kubernetes API server without a real cluster?
> 2. How does `suite_test.go` bootstrap the test environment in BeforeSuite?
> 3. In the controller test, how is a fake ImageSync resource created and reconciled?
> 4. Why does the test use `fake-registry.invalid` instead of a real registry?
> 5. How does `record.NewFakeRecorder` capture Kubernetes events in tests?
> 6. What are Ginkgo's `Describe`, `Context`, `It`, `BeforeEach`, and `Eventually` for?
>
> Explain Go testing conventions and how `go test` discovers test files.

**What you'll learn:** Kubernetes operator testing patterns, envtest, Ginkgo BDD framework, fake recorders, test isolation.

---

## Part 10: The Build System

### File: `Makefile`

**Prompt:**
> This is the Makefile for a Kubebuilder-based Kubernetes operator. Explain the key targets:
> 1. `make build` — what steps run before the Go compile?
> 2. `make test` — how does envtest get set up? What does KUBEBUILDER_ASSETS do?
> 3. `make manifests` — what does controller-gen generate and from what?
> 4. `make generate` — what are DeepCopy methods and why are they generated?
> 5. `make install` vs `make deploy` — what's the difference?
> 6. `make docker-build` vs `make docker-buildx` — when to use each?
>
> Explain how Make works for someone who hasn't used it before.

**What you'll learn:** Go build toolchain, CRD generation from code, envtest setup, kustomize-based deployment, Make basics.

---

## Suggested Order

If you want the shortest path to understanding:

1. **Part 2** (CRD types) — understand what users define
2. **Part 3** (Reconciler) — understand the core logic
3. **Part 4** (Auth) — understand how registries are accessed
4. **Part 5** (Copier) — understand what actually copies images
5. **Part 1** (main.go) — understand how it all boots up
6. **Part 6-8** (Schedule, ECR, Metrics) — supporting subsystems
7. **Part 9-10** (Testing, Build) — development workflow

Or follow Parts 1-10 in order for the full startup-to-sync execution trace.
