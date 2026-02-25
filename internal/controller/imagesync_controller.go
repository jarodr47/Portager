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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

// ImageSyncReconciler reconciles a ImageSync object
type ImageSyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=portager.portager.io,resources=imagesyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=portager.portager.io,resources=imagesyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=portager.portager.io,resources=imagesyncs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ImageSync object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
func (r *ImageSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ImageSync resource that triggered this reconciliation.
	// req.NamespacedName tells us which object changed — we need to GET it
	// from the API server to see its current spec.
	var imageSync portagerv1alpha1.ImageSync
	if err := r.Get(ctx, req.NamespacedName, &imageSync); err != nil {
		if errors.IsNotFound(err) {
			// The resource was deleted — nothing to do.
			log.Info("ImageSync resource not found, likely deleted")
			return ctrl.Result{}, nil
		}
		// A real error (network issue, RBAC problem, etc.)
		return ctrl.Result{}, fmt.Errorf("failed to get ImageSync: %w", err)
	}

	// Log what we found — this is our "hello world" proof that reconciliation works.
	log.Info("Reconciling ImageSync",
		"name", imageSync.Name,
		"namespace", imageSync.Namespace,
		"source", imageSync.Spec.Source.Registry,
		"destination", imageSync.Spec.Destination.Registry,
		"imageCount", len(imageSync.Spec.Images),
		"schedule", imageSync.Spec.Schedule,
	)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&portagerv1alpha1.ImageSync{}).
		Named("imagesync").
		Complete(r)
}
