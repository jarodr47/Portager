/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
	"github.com/jarodr47/portager/internal/controller/auth"
	portageMetrics "github.com/jarodr47/portager/internal/controller/metrics"
	"github.com/jarodr47/portager/internal/controller/registry"
	"github.com/jarodr47/portager/internal/controller/schedule"
	"github.com/jarodr47/portager/internal/controller/sync"
)

// SyncNowAnnotation is the annotation key users set to "true" to trigger an
// immediate sync, bypassing the cron schedule. The controller removes the
// annotation after processing so it acts as a one-shot trigger.
const SyncNowAnnotation = "portager.portager.io/sync-now"

// ImageSyncReconciler reconciles a ImageSync object
type ImageSyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Recorder emits Kubernetes Events for observability.
	// In production, this writes to the API server (visible via kubectl describe).
	// In tests, we swap in a FakeRecorder that captures events in a Go channel.
	Recorder record.EventRecorder

	// Scheduler parses cron expressions and computes the next sync time.
	Scheduler *schedule.Scheduler
}

// +kubebuilder:rbac:groups=portager.portager.io,resources=imagesyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=portager.portager.io,resources=imagesyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=portager.portager.io,resources=imagesyncs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile copies container images from the source registry to the destination
// registry as defined in the ImageSync spec. It uses digest comparison to skip
// images that are already up-to-date in the destination.
func (r *ImageSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	reconcileStart := time.Now()

	// 1. Fetch the ImageSync resource that triggered this reconciliation.
	var imageSync portagerv1alpha1.ImageSync
	if err := r.Get(ctx, req.NamespacedName, &imageSync); err != nil {
		if errors.IsNotFound(err) {
			log.Info("ImageSync resource not found, likely deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ImageSync: %w", err)
	}

	log.Info("Reconciling ImageSync",
		"name", imageSync.Name,
		"source", imageSync.Spec.Source.Registry,
		"destination", imageSync.Spec.Destination.Registry,
		"imageCount", len(imageSync.Spec.Images),
	)

	// 2. Validate the cron schedule expression.
	if err := r.Scheduler.Validate(imageSync.Spec.Schedule); err != nil {
		return r.updateStatusWithError(ctx, &imageSync, "InvalidSchedule",
			fmt.Sprintf("invalid schedule: %v", err))
	}

	// 3. Check for sync-now annotation (bypasses schedule).
	syncNow := false
	if imageSync.Annotations[SyncNowAnnotation] == "true" {
		log.Info("sync-now annotation detected, bypassing schedule")
		delete(imageSync.Annotations, SyncNowAnnotation)
		if err := r.Update(ctx, &imageSync); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove sync-now annotation: %w", err)
		}
		// r.Update() already updates imageSync in place with the fresh
		// ResourceVersion from the API server. Do NOT re-fetch from the
		// informer cache here — the cache is eventually consistent and may
		// return a stale ResourceVersion, causing a conflict on the status
		// write later.
		r.Recorder.Event(&imageSync, corev1.EventTypeNormal, "SyncNowTriggered",
			"Immediate sync triggered via annotation")
		syncNow = true
	}

	// 4. Schedule check — skip if sync-now was triggered or spec changed.
	if !syncNow {
		specChanged := imageSync.Generation != imageSync.Status.ObservedGeneration
		if !specChanged && imageSync.Status.NextSyncTime != nil {
			nextSync := imageSync.Status.NextSyncTime.Time
			if time.Now().Before(nextSync) {
				requeueAfter := time.Until(nextSync)
				log.Info("Sync not due yet, requeuing",
					"nextSyncTime", nextSync,
					"requeueAfter", requeueAfter,
				)
				return ctrl.Result{RequeueAfter: requeueAfter}, nil
			}
		}
		if specChanged {
			log.Info("Spec generation changed, syncing immediately",
				"generation", imageSync.Generation,
				"observedGeneration", imageSync.Status.ObservedGeneration,
			)
		}
		// NextSyncTime is nil (first sync) or in the past (due) or spec changed — proceed.
	}

	// 5. Build authenticators for source and destination registries.
	srcAuth := r.buildSourceAuth(&imageSync)
	dstAuth, err := r.buildDestAuth(ctx, &imageSync)
	if err != nil {
		return r.updateStatusWithError(ctx, &imageSync, "AuthFailed",
			fmt.Sprintf("destination auth setup failed: %v", err))
	}

	srcAuthn, err := srcAuth.Authenticate(ctx)
	if err != nil {
		return r.updateStatusWithError(ctx, &imageSync, "AuthFailed",
			fmt.Sprintf("source auth failed: %v", err))
	}
	dstAuthn, err := dstAuth.Authenticate(ctx)
	if err != nil {
		return r.updateStatusWithError(ctx, &imageSync, "AuthFailed",
			fmt.Sprintf("destination auth failed: %v", err))
	}

	// 6. Create destination repositories if needed (ECR only).
	if imageSync.Spec.CreateDestinationRepos && imageSync.Spec.Destination.Auth.Method == "ecr" {
		region, err := auth.ParseECRRegion(imageSync.Spec.Destination.Registry)
		if err != nil {
			return r.updateStatusWithError(ctx, &imageSync, "RepoCreationFailed",
				fmt.Sprintf("parsing ECR region for repo creation: %v", err))
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return r.updateStatusWithError(ctx, &imageSync, "RepoCreationFailed",
				fmt.Sprintf("loading AWS config for repo creation: %v", err))
		}
		repoMgr := &registry.ECRRepoManager{Client: ecr.NewFromConfig(cfg)}

		// Deduplicate repo names (prefix + image name).
		seen := make(map[string]struct{})
		for _, image := range imageSync.Spec.Images {
			repoName := image.Name
			if imageSync.Spec.Destination.RepositoryPrefix != "" {
				repoName = imageSync.Spec.Destination.RepositoryPrefix + "/" + image.Name
			}
			if _, ok := seen[repoName]; ok {
				continue
			}
			seen[repoName] = struct{}{}

			if err := repoMgr.EnsureRepositoryExists(ctx, repoName); err != nil {
				return r.updateStatusWithError(ctx, &imageSync, "RepoCreationFailed",
					fmt.Sprintf("ensuring ECR repository %q exists: %v", repoName, err))
			}
			r.Recorder.Eventf(&imageSync, corev1.EventTypeNormal, "RepoEnsured",
				"ECR repository %q exists or was created", repoName)
		}
	}

	// 7. For each image+tag: compare digests, copy if needed, build per-image status.
	copier := &sync.ImageCopier{}
	var (
		copyErrors   []error
		imageResults []portagerv1alpha1.ImageSyncStatusImage
		totalCount   int
		syncedCount  int
		failedCount  int
	)

	for _, image := range imageSync.Spec.Images {
		imageStatus := portagerv1alpha1.ImageSyncStatusImage{
			Name: image.Name,
		}

		for _, tag := range image.Tags {
			totalCount++
			now := metav1.Now()

			srcRef := fmt.Sprintf("%s/%s:%s", imageSync.Spec.Source.Registry, image.Name, tag)
			dstRef := buildDestRef(imageSync.Spec.Destination, image.Name, tag)

			tagStatus := portagerv1alpha1.TagSyncStatus{
				Tag:          tag,
				LastSyncTime: &now,
			}

			// 4a. Get the source digest.
			srcDigest, err := copier.GetDigest(ctx, srcRef, srcAuthn)
			if err != nil {
				failedCount++
				tagStatus.Synced = false
				tagStatus.Error = fmt.Sprintf("failed to get source digest: %v", err)
				copyErrors = append(copyErrors, err)
				log.Error(err, "Failed to get source digest", "source", srcRef)
				r.Recorder.Eventf(&imageSync, corev1.EventTypeWarning, "SyncFailed",
					"Failed to get source digest for %s: %v", srcRef, err)
				portageMetrics.ImagesFailed.WithLabelValues(imageSync.Name, imageSync.Namespace).Inc()
				imageStatus.Tags = append(imageStatus.Tags, tagStatus)
				continue
			}
			tagStatus.SourceDigest = srcDigest

			// 4b. Get the destination digest (may not exist yet).
			dstDigest, err := copier.GetDigest(ctx, dstRef, dstAuthn)

			// 3c. Compare digests. If they match, skip the copy.
			if err == nil && srcDigest == dstDigest {
				syncedCount++
				tagStatus.Synced = true
				// V(1) = debug level. Only visible with -v=1 or higher.
				log.V(1).Info("Image already up-to-date, skipping copy",
					"image", srcRef, "digest", srcDigest)
				r.Recorder.Eventf(&imageSync, corev1.EventTypeNormal, "ImageSkipped",
					"Image %s already up-to-date (digest: %s)", srcRef, truncateDigest(srcDigest))
				portageMetrics.ImagesSkipped.WithLabelValues(imageSync.Name, imageSync.Namespace).Inc()
				imageStatus.Tags = append(imageStatus.Tags, tagStatus)
				continue
			}

			// 4d. Digests differ or destination doesn't exist — copy the image.
			if err := copier.Copy(ctx, srcRef, dstRef, srcAuthn, dstAuthn); err != nil {
				failedCount++
				tagStatus.Synced = false
				tagStatus.Error = fmt.Sprintf("copy failed: %v", err)
				copyErrors = append(copyErrors, err)
				log.Error(err, "Failed to copy image", "source", srcRef, "destination", dstRef)
				r.Recorder.Eventf(&imageSync, corev1.EventTypeWarning, "SyncFailed",
					"Failed to copy %s to %s: %v", srcRef, dstRef, err)
				portageMetrics.ImagesFailed.WithLabelValues(imageSync.Name, imageSync.Namespace).Inc()
			} else {
				syncedCount++
				tagStatus.Synced = true
				log.Info("Successfully synced image", "source", srcRef, "destination", dstRef,
					"digest", srcDigest)
				r.Recorder.Eventf(&imageSync, corev1.EventTypeNormal, "ImageSynced",
					"Synced %s → %s (digest: %s)", srcRef, dstRef, truncateDigest(srcDigest))
				portageMetrics.ImagesCopied.WithLabelValues(imageSync.Name, imageSync.Namespace).Inc()
			}

			imageStatus.Tags = append(imageStatus.Tags, tagStatus)
		}

		imageResults = append(imageResults, imageStatus)
	}

	// 4. Update status with results.
	now := metav1.Now()
	imageSync.Status.LastSyncTime = &now
	imageSync.Status.Images = imageResults
	imageSync.Status.TotalImages = totalCount
	imageSync.Status.SyncedImages = syncedCount
	imageSync.Status.FailedImages = failedCount
	imageSync.Status.ObservedGeneration = imageSync.Generation

	// Mark Syncing=False in the final status write (not as a separate update,
	// which would trigger an extra reconcile cycle).
	meta.SetStatusCondition(&imageSync.Status.Conditions, metav1.Condition{
		Type:               "Syncing",
		Status:             metav1.ConditionFalse,
		Reason:             "SyncComplete",
		Message:            "Image sync finished",
		ObservedGeneration: imageSync.Generation,
	})

	if len(copyErrors) == 0 {
		meta.SetStatusCondition(&imageSync.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "SyncSucceeded",
			Message:            fmt.Sprintf("All %d image(s) synced successfully", totalCount),
			ObservedGeneration: imageSync.Generation,
		})

		// Calculate the next scheduled sync time. Only advance on success —
		// on failure, leave nextSyncTime in the past so the backoff requeue
		// re-enters reconcile and the schedule check allows a retry.
		nextSync, err := r.Scheduler.NextSyncTime(imageSync.Spec.Schedule, now.Time)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to compute next sync time: %w", err)
		}
		nextSyncMeta := metav1.NewTime(nextSync)
		imageSync.Status.NextSyncTime = &nextSyncMeta
	} else {
		meta.SetStatusCondition(&imageSync.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "SyncFailed",
			Message:            fmt.Sprintf("%d of %d image(s) failed to sync", failedCount, totalCount),
			ObservedGeneration: imageSync.Generation,
		})
	}

	if err := r.Status().Update(ctx, &imageSync); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ImageSync status: %w", err)
	}

	// Emit a summary event.
	r.Recorder.Eventf(&imageSync, corev1.EventTypeNormal, "SyncComplete",
		"Sync complete: %d synced, %d failed, %d total", syncedCount, failedCount, totalCount)

	// Record Prometheus metrics.
	portageMetrics.SyncDuration.WithLabelValues(imageSync.Name, imageSync.Namespace).Observe(time.Since(reconcileStart).Seconds())
	syncStatus := "success"
	if len(copyErrors) > 0 {
		syncStatus = "failure"
	}
	portageMetrics.SyncTotal.WithLabelValues(imageSync.Name, imageSync.Namespace, syncStatus).Inc()
	portageMetrics.ImageInfo.WithLabelValues(
		imageSync.Name, imageSync.Namespace,
		strconv.Itoa(syncedCount), strconv.Itoa(failedCount), strconv.Itoa(totalCount),
	).Set(1)

	// If any copies failed, return an error so controller-runtime requeues
	// with backoff. Update status first so the user can see what failed.
	if len(copyErrors) > 0 {
		return ctrl.Result{}, fmt.Errorf("%d image(s) failed to sync", len(copyErrors))
	}

	// Requeue for the next scheduled sync.
	requeueAfter := time.Until(imageSync.Status.NextSyncTime.Time)
	log.Info("Sync succeeded, requeuing for next schedule",
		"nextSyncTime", imageSync.Status.NextSyncTime.Time,
		"requeueAfter", requeueAfter,
	)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// buildSourceAuth returns an Authenticator for the source registry.
// If no authSecretRef is configured, returns anonymous (for public registries).
func (r *ImageSyncReconciler) buildSourceAuth(is *portagerv1alpha1.ImageSync) auth.Authenticator {
	if is.Spec.Source.AuthSecretRef == nil {
		return &auth.AnonymousAuthenticator{}
	}

	ns := is.Spec.Source.AuthSecretRef.Namespace
	if ns == "" {
		ns = is.Namespace // default to the ImageSync's own namespace
	}

	return &auth.SecretAuthenticator{
		Client:    r.Client,
		SecretKey: types.NamespacedName{Name: is.Spec.Source.AuthSecretRef.Name, Namespace: ns},
		Registry:  is.Spec.Source.Registry,
	}
}

// buildDestAuth returns an Authenticator for the destination registry.
// For "secret" method with a secretRef, uses SecretAuthenticator.
// For "ecr" method, uses IRSA-based ECR authentication.
// Otherwise returns anonymous (e.g., local registry with no auth).
func (r *ImageSyncReconciler) buildDestAuth(ctx context.Context, is *portagerv1alpha1.ImageSync) (auth.Authenticator, error) {
	switch is.Spec.Destination.Auth.Method {
	case "secret":
		if is.Spec.Destination.Auth.SecretRef != nil {
			ns := is.Spec.Destination.Auth.SecretRef.Namespace
			if ns == "" {
				ns = is.Namespace
			}
			return &auth.SecretAuthenticator{
				Client:    r.Client,
				SecretKey: types.NamespacedName{Name: is.Spec.Destination.Auth.SecretRef.Name, Namespace: ns},
				Registry:  is.Spec.Destination.Registry,
			}, nil
		}
	case "ecr":
		region, err := auth.ParseECRRegion(is.Spec.Destination.Registry)
		if err != nil {
			return nil, fmt.Errorf("parsing ECR region: %w", err)
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("loading AWS config: %w", err)
		}
		return &auth.ECRAuthenticator{Client: ecr.NewFromConfig(cfg)}, nil
	case "anonymous":
		return &auth.AnonymousAuthenticator{}, nil
	}
	return &auth.AnonymousAuthenticator{}, nil
}

// buildDestRef constructs the full destination image reference.
// If a repositoryPrefix is set, it's inserted between the registry and image name.
// Example: registry=ecr.aws/acct, prefix=chainguard, name=go, tag=1.22
//
//	→ ecr.aws/acct/chainguard/go:1.22
func buildDestRef(dest portagerv1alpha1.DestinationConfig, imageName, tag string) string {
	if dest.RepositoryPrefix != "" {
		return fmt.Sprintf("%s/%s/%s:%s", dest.Registry, dest.RepositoryPrefix, imageName, tag)
	}
	return fmt.Sprintf("%s/%s:%s", dest.Registry, imageName, tag)
}

// truncateDigest shortens a digest for display in events.
// "sha256:abcdef1234567890..." → "sha256:abcdef12"
func truncateDigest(digest string) string {
	parts := strings.SplitN(digest, ":", 2)
	if len(parts) == 2 && len(parts[1]) > 8 {
		return parts[0] + ":" + parts[1][:8]
	}
	return digest
}

// updateStatusWithError sets a failed Ready condition and persists status.
// Used when authentication or other pre-copy steps fail.
func (r *ImageSyncReconciler) updateStatusWithError(
	ctx context.Context, is *portagerv1alpha1.ImageSync, reason, message string,
) (ctrl.Result, error) {
	meta.SetStatusCondition(&is.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: is.Generation,
	})
	if err := r.Status().Update(ctx, is); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update ImageSync status: %w", err)
	}
	return ctrl.Result{}, fmt.Errorf("%s: %s", reason, message)
}

// syncNowAnnotationPredicate triggers reconciliation when a user adds the
// sync-now annotation. Annotation changes do NOT increment metadata.generation,
// so GenerationChangedPredicate alone won't detect them.
//
// Only Update events matter: Create and Delete are handled by
// GenerationChangedPredicate, and Generic events aren't used.
type syncNowAnnotationPredicate struct{}

func (syncNowAnnotationPredicate) Create(_ event.CreateEvent) bool   { return false }
func (syncNowAnnotationPredicate) Delete(_ event.DeleteEvent) bool   { return false }
func (syncNowAnnotationPredicate) Generic(_ event.GenericEvent) bool { return false }

// Update returns true only when the new object has the sync-now annotation
// set to "true" and the old object did not. This prevents matching on the
// removal of the annotation (which the controller does after processing).
func (syncNowAnnotationPredicate) Update(e event.UpdateEvent) bool {
	oldAnnotations := e.ObjectOld.GetAnnotations()
	newAnnotations := e.ObjectNew.GetAnnotations()
	return newAnnotations[SyncNowAnnotation] == "true" && oldAnnotations[SyncNowAnnotation] != "true"
}

// SetupWithManager sets up the controller with the Manager.
//
// Two predicates are composed with Or:
//   - GenerationChangedPredicate: reconcile on spec changes (generation increments)
//   - syncNowAnnotationPredicate: reconcile when sync-now annotation is added
//
// Status-only updates are still filtered out — they don't change generation
// and don't add the sync-now annotation.
func (r *ImageSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&portagerv1alpha1.ImageSync{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				syncNowAnnotationPredicate{},
			))).
		Named("imagesync").
		Complete(r)
}
