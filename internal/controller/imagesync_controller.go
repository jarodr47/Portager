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
	"strings"

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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
	"github.com/jarodr47/portager/internal/controller/auth"
	"github.com/jarodr47/portager/internal/controller/sync"
)

// ImageSyncReconciler reconciles a ImageSync object
type ImageSyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Recorder emits Kubernetes Events for observability.
	// In production, this writes to the API server (visible via kubectl describe).
	// In tests, we swap in a FakeRecorder that captures events in a Go channel.
	Recorder record.EventRecorder
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

	// 2. Build authenticators for source and destination registries.
	srcAuth := r.buildSourceAuth(&imageSync)
	dstAuth := r.buildDestAuth(&imageSync)

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

	// 3. For each image+tag: compare digests, copy if needed, build per-image status.
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
			} else {
				syncedCount++
				tagStatus.Synced = true
				log.Info("Successfully synced image", "source", srcRef, "destination", dstRef,
					"digest", srcDigest)
				r.Recorder.Eventf(&imageSync, corev1.EventTypeNormal, "ImageSynced",
					"Synced %s → %s (digest: %s)", srcRef, dstRef, truncateDigest(srcDigest))
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

	// If any copies failed, return an error so controller-runtime requeues
	// with backoff. Update status first so the user can see what failed.
	if len(copyErrors) > 0 {
		return ctrl.Result{}, fmt.Errorf("%d image(s) failed to sync", len(copyErrors))
	}

	return ctrl.Result{}, nil
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
// If method is "secret" with a secretRef, uses SecretAuthenticator.
// Otherwise returns anonymous (e.g., local registry with no auth).
func (r *ImageSyncReconciler) buildDestAuth(is *portagerv1alpha1.ImageSync) auth.Authenticator {
	if is.Spec.Destination.Auth.Method == "secret" && is.Spec.Destination.Auth.SecretRef != nil {
		ns := is.Spec.Destination.Auth.SecretRef.Namespace
		if ns == "" {
			ns = is.Namespace
		}
		return &auth.SecretAuthenticator{
			Client:    r.Client,
			SecretKey: types.NamespacedName{Name: is.Spec.Destination.Auth.SecretRef.Name, Namespace: ns},
			Registry:  is.Spec.Destination.Registry,
		}
	}
	return &auth.AnonymousAuthenticator{}
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

// SetupWithManager sets up the controller with the Manager.
//
// GenerationChangedPredicate ensures we only reconcile when the spec changes
// (generation increments), NOT when we write status updates. Without this,
// every status write triggers a re-reconcile, creating a loop.
func (r *ImageSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&portagerv1alpha1.ImageSync{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("imagesync").
		Complete(r)
}
