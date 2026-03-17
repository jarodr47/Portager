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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretReference identifies a Kubernetes Secret by name and optional namespace.
// If Namespace is omitted, it defaults to the namespace of the ImageSync resource.
type SecretReference struct {
	// name of the Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// namespace of the Secret. Defaults to the ImageSync's namespace if omitted.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// SourceConfig defines where to pull images from.
type SourceConfig struct {
	// registry is the source registry host (e.g., "cgr.dev/my-org", "docker.io/library").
	// No scheme — just the host and optional path prefix.
	// +kubebuilder:validation:Required
	Registry string `json:"registry"`

	// authSecretRef references a kubernetes.io/dockerconfigjson Secret for pull authentication.
	// Omit for public registries.
	// +optional
	AuthSecretRef *SecretReference `json:"authSecretRef,omitempty"`
}

// AuthConfig defines how the controller authenticates to the destination registry.
type AuthConfig struct {
	// method specifies the authentication strategy.
	// "secret" uses a dockerconfigjson Secret; "ecr" uses IRSA for AWS ECR.
	// +kubebuilder:validation:Enum=secret;ecr
	// +kubebuilder:validation:Required
	Method string `json:"method"`

	// secretRef references a kubernetes.io/dockerconfigjson Secret.
	// Required when method is "secret".
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// DestinationConfig defines where to push images to.
type DestinationConfig struct {
	// registry is the destination registry host
	// (e.g., "123456789012.dkr.ecr.us-gov-west-1.amazonaws.com").
	// +kubebuilder:validation:Required
	Registry string `json:"registry"`

	// auth configures how the controller authenticates to push images.
	// +kubebuilder:validation:Required
	Auth AuthConfig `json:"auth"`

	// repositoryPrefix is prepended to image names in the destination.
	// For example, with prefix "chainguard", image "go" becomes "chainguard/go".
	// +optional
	RepositoryPrefix string `json:"repositoryPrefix,omitempty"`
}

// ImageSpec defines a single image to sync, including which tags to copy.
type ImageSpec struct {
	// name is the image name relative to the source registry
	// (e.g., "go", "node", "python").
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// tags is the list of image tags to sync (e.g., ["latest", "1.22"]).
	// +kubebuilder:validation:MinItems=1
	Tags []string `json:"tags"`
}

// ImageSyncSpec defines the desired state of ImageSync — which images to sync,
// where to pull from, where to push to, and on what schedule.
type ImageSyncSpec struct {
	// schedule is a cron expression or shorthand (e.g., "0 */6 * * *", "@every 1h")
	// controlling how often images are synced.
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`

	// source defines the registry to pull images from.
	// +kubebuilder:validation:Required
	Source SourceConfig `json:"source"`

	// destination defines the registry to push images to.
	// +kubebuilder:validation:Required
	Destination DestinationConfig `json:"destination"`

	// images is the list of images to sync from source to destination.
	// +kubebuilder:validation:MinItems=1
	Images []ImageSpec `json:"images"`

	// createDestinationRepos, when true, causes the controller to create
	// destination repositories before pushing (currently ECR only).
	// +optional
	CreateDestinationRepos bool `json:"createDestinationRepos,omitempty"`
}

// TagSyncStatus records the result of syncing a single image tag.
type TagSyncStatus struct {
	// tag is the image tag that was synced (e.g., "latest", "1.22").
	Tag string `json:"tag"`

	// synced indicates whether this tag was successfully copied.
	Synced bool `json:"synced"`

	// sourceDigest is the manifest digest of the source image (e.g., "sha256:abc123...").
	// Used for digest comparison to skip already-synced images.
	// +optional
	SourceDigest string `json:"sourceDigest,omitempty"`

	// lastSyncTime is when this specific tag was last synced.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// error contains the failure reason if synced is false.
	// +optional
	Error string `json:"error,omitempty"`
}

// ImageSyncStatusImage records the sync status for all tags of a single image.
type ImageSyncStatusImage struct {
	// name is the image name (e.g., "alpine", "go").
	Name string `json:"name"`

	// tags contains per-tag sync results.
	Tags []TagSyncStatus `json:"tags,omitempty"`
}

// ImageSyncStatus defines the observed state of ImageSync.
type ImageSyncStatus struct {
	// lastSyncTime is the timestamp of the most recent sync attempt.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// nextSyncTime is the calculated time of the next scheduled sync.
	// +optional
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`

	// conditions represent the current state of the ImageSync resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// images contains per-image sync status details.
	// +optional
	Images []ImageSyncStatusImage `json:"images,omitempty"`

	// totalImages is the total number of image+tag combinations to sync.
	// +optional
	TotalImages int `json:"totalImages,omitempty"`

	// syncedImages is the number of image+tag combinations successfully synced or already up-to-date.
	// +optional
	SyncedImages int `json:"syncedImages,omitempty"`

	// failedImages is the number of image+tag combinations that failed to sync.
	// +optional
	FailedImages int `json:"failedImages,omitempty"`

	// observedGeneration is the most recent generation observed by the controller.
	// When this differs from metadata.generation, the controller syncs immediately
	// regardless of schedule.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ImageSync is the Schema for the imagesyncs API
type ImageSync struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ImageSync
	// +required
	Spec ImageSyncSpec `json:"spec"`

	// status defines the observed state of ImageSync
	// +optional
	Status ImageSyncStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ImageSyncList contains a list of ImageSync
type ImageSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ImageSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImageSync{}, &ImageSyncList{})
}
