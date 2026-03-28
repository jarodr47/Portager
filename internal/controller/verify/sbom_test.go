package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

func TestSbomChecker_CheckSbom(t *testing.T) {
	tests := []struct {
		name        string
		config      *portagerv1alpha1.SbomGateConfig
		fetcher     *mockReferrersFetcher
		wantErr     bool
		errContains string
	}{
		{
			name:    "nil config returns success",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "disabled returns success",
			config:  &portagerv1alpha1.SbomGateConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:   "SPDX SBOM found returns success",
			config: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			fetcher: &mockReferrersFetcher{
				referrers: []Referrer{
					{ArtifactType: spdxArtifactType, Digest: "sha256:abc123"},
				},
			},
			wantErr: false,
		},
		{
			name:   "CycloneDX SBOM found returns success",
			config: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			fetcher: &mockReferrersFetcher{
				referrers: []Referrer{
					{ArtifactType: cyclonedxArtifactType, Digest: "sha256:abc123"},
				},
			},
			wantErr: false,
		},
		{
			name:   "no SBOM returns error",
			config: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			fetcher: &mockReferrersFetcher{
				referrers: []Referrer{},
			},
			wantErr:     true,
			errContains: "no SBOM found",
		},
		{
			name:   "non-SBOM referrer only returns error",
			config: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			fetcher: &mockReferrersFetcher{
				referrers: []Referrer{
					{ArtifactType: sarifArtifactType, Digest: "sha256:abc123"},
				},
			},
			wantErr:     true,
			errContains: "no SBOM found",
		},
		{
			name:   "SBOM among other referrers returns success",
			config: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			fetcher: &mockReferrersFetcher{
				referrers: []Referrer{
					{ArtifactType: sarifArtifactType, Digest: "sha256:sarif"},
					{ArtifactType: spdxArtifactType, Digest: "sha256:sbom"},
				},
			},
			wantErr: false,
		},
		{
			name:   "fetch error propagates",
			config: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			fetcher: &mockReferrersFetcher{
				fetchErr: errors.New("registry unavailable"),
			},
			wantErr:     true,
			errContains: "registry unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &sbomChecker{fetcher: tt.fetcher}
			err := checker.CheckSbom(
				context.Background(),
				"fake-registry.invalid/test@sha256:abc123def456",
				tt.config,
				authn.Anonymous,
			)

			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected success but got error: %v", err)
			}
			if tt.errContains != "" && err != nil {
				if !containsSubstring(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}
