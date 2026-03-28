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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// SyncTotal counts total reconcile completions, labeled by outcome.
	SyncTotal = promauto.With(metrics.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "portage_sync_total",
			Help: "Total number of ImageSync reconcile completions",
		},
		[]string{"name", "namespace", "status"},
	)

	// SyncDuration observes how long each reconcile cycle takes.
	SyncDuration = promauto.With(metrics.Registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "portage_sync_duration_seconds",
			Help:    "Duration of ImageSync reconcile cycles in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"name", "namespace"},
	)

	// ImagesCopied counts individual images successfully copied.
	ImagesCopied = promauto.With(metrics.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "portage_images_copied_total",
			Help: "Total number of individual images copied",
		},
		[]string{"name", "namespace"},
	)

	// ImagesSkipped counts images skipped due to matching digests.
	ImagesSkipped = promauto.With(metrics.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "portage_images_skipped_total",
			Help: "Total number of images skipped (digest match)",
		},
		[]string{"name", "namespace"},
	)

	// ImagesFailed counts individual image copy failures.
	ImagesFailed = promauto.With(metrics.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "portage_images_failed_total",
			Help: "Total number of individual image copy failures",
		},
		[]string{"name", "namespace"},
	)

	// ImageInfo provides a current-state snapshot per ImageSync resource.
	ImageInfo = promauto.With(metrics.Registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "portage_image_info",
			Help: "Current state snapshot per ImageSync resource",
		},
		[]string{"name", "namespace", "synced", "failed", "total"},
	)

	// ImagesValidationFailed counts images that failed pre-sync validation.
	ImagesValidationFailed = promauto.With(metrics.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "portage_images_validation_failed_total",
			Help: "Total number of images that failed pre-sync validation",
		},
		[]string{"name", "namespace", "gate"},
	)

	// ImagesVerified counts images that passed pre-sync validation.
	ImagesVerified = promauto.With(metrics.Registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "portage_images_verified_total",
			Help: "Total number of images that passed pre-sync validation",
		},
		[]string{"name", "namespace"},
	)
)
