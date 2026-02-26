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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

var _ = Describe("ImageSync Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		imagesync := &portagerv1alpha1.ImageSync{}

		// FakeRecorder captures Kubernetes Events in a Go channel instead
		// of writing them to the API server. The buffer size (100) is generous
		// so tests don't block if we emit many events.
		var fakeRecorder *record.FakeRecorder

		BeforeEach(func() {
			fakeRecorder = record.NewFakeRecorder(100)

			By("creating the custom resource for the Kind ImageSync")
			err := k8sClient.Get(ctx, typeNamespacedName, imagesync)
			if err != nil && errors.IsNotFound(err) {
				resource := &portagerv1alpha1.ImageSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: portagerv1alpha1.ImageSyncSpec{
						Schedule: "@every 1h",
						Source: portagerv1alpha1.SourceConfig{
							// Use a fake unreachable registry so GetDigest fails fast
							// instead of making real network calls to Docker Hub.
							Registry: "fake-registry.invalid",
						},
						Destination: portagerv1alpha1.DestinationConfig{
							Registry: "localhost:5000",
							Auth: portagerv1alpha1.AuthConfig{
								Method: "secret",
							},
						},
						Images: []portagerv1alpha1.ImageSpec{
							{
								Name: "alpine",
								Tags: []string{"latest"},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &portagerv1alpha1.ImageSync{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ImageSync")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile and update status with failure when registries are unreachable", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ImageSyncReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: fakeRecorder,
			}

			// In envtest there are no real registries, so the digest check will fail.
			// The reconciler should handle this gracefully: update status with
			// the failure condition, then return an error for requeue.
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())

			// Re-fetch the resource to see the updated status.
			Expect(k8sClient.Get(ctx, typeNamespacedName, imagesync)).To(Succeed())

			// Verify status was updated despite the copy failure.
			Expect(imagesync.Status.LastSyncTime).NotTo(BeNil())

			// Verify Ready condition is False with SyncFailed reason.
			readyCond := meta.FindStatusCondition(imagesync.Status.Conditions, "Ready")
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal("SyncFailed"))

			// Verify Syncing condition is False (sync is complete).
			syncingCond := meta.FindStatusCondition(imagesync.Status.Conditions, "Syncing")
			Expect(syncingCond).NotTo(BeNil())
			Expect(syncingCond.Status).To(Equal(metav1.ConditionFalse))

			// Verify per-image status was populated.
			Expect(imagesync.Status.Images).To(HaveLen(1))
			Expect(imagesync.Status.Images[0].Name).To(Equal("alpine"))
			Expect(imagesync.Status.Images[0].Tags).To(HaveLen(1))
			Expect(imagesync.Status.Images[0].Tags[0].Tag).To(Equal("latest"))
			Expect(imagesync.Status.Images[0].Tags[0].Synced).To(BeFalse())
			// With a fake registry, the error should mention the source digest failure.
			Expect(imagesync.Status.Images[0].Tags[0].Error).To(ContainSubstring("failed to get source digest"))

			// Verify summary counts.
			Expect(imagesync.Status.TotalImages).To(Equal(1))
			Expect(imagesync.Status.FailedImages).To(Equal(1))
			Expect(imagesync.Status.SyncedImages).To(Equal(0))

			// Verify events were emitted.
			// Receive() reads one item from the channel and checks it against the matcher.
			// ContainSubstring matches if the event string contains the expected text.
			Expect(fakeRecorder.Events).To(Receive(ContainSubstring("SyncFailed")))
			Expect(fakeRecorder.Events).To(Receive(ContainSubstring("SyncComplete")))
		})
	})
})
