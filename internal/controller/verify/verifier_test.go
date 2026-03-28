package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

// mockVulnChecker implements VulnerabilityChecker for testing.
type mockVulnChecker struct {
	err error
}

func (m *mockVulnChecker) CheckVulnerabilities(_ context.Context, _ string, _ *portagerv1alpha1.VulnerabilityGateConfig, _ authn.Authenticator) error {
	return m.err
}

// mockSbomChecker implements SbomChecker for testing.
type mockSbomChecker struct {
	err error
}

func (m *mockSbomChecker) CheckSbom(_ context.Context, _ string, _ *portagerv1alpha1.SbomGateConfig, _ authn.Authenticator) error {
	return m.err
}

func TestValidator_Validate(t *testing.T) {
	tests := []struct {
		name       string
		config     *portagerv1alpha1.ValidationConfig
		cosignErr  error
		vulnErr    error
		sbomErr    error
		wantVerify bool
		wantErr    bool
	}{
		{
			name:       "nil config returns verified",
			config:     nil,
			wantVerify: true,
		},
		{
			name: "both gates disabled returns verified",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign:            &portagerv1alpha1.CosignConfig{Enabled: false},
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{Enabled: false},
			},
			wantVerify: true,
		},
		{
			name: "cosign passes and vuln passes returns verified",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign: &portagerv1alpha1.CosignConfig{
					Enabled:   true,
					PublicKey: "test-key",
				},
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{
					Enabled:     true,
					MaxSeverity: "high",
				},
			},
			wantVerify: true,
		},
		{
			name: "cosign fails stops validation early",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign: &portagerv1alpha1.CosignConfig{
					Enabled:   true,
					PublicKey: "test-key",
				},
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{
					Enabled:     true,
					MaxSeverity: "high",
				},
			},
			cosignErr:  errors.New("no matching signatures"),
			wantVerify: false,
			wantErr:    true,
		},
		{
			name: "cosign passes but vuln fails",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign: &portagerv1alpha1.CosignConfig{
					Enabled:   true,
					PublicKey: "test-key",
				},
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{
					Enabled:     true,
					MaxSeverity: "high",
				},
			},
			vulnErr:    errors.New("CVE-2024-001 (critical)"),
			wantVerify: false,
			wantErr:    true,
		},
		{
			name: "only cosign enabled and passes",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign: &portagerv1alpha1.CosignConfig{
					Enabled:   true,
					PublicKey: "test-key",
				},
			},
			wantVerify: true,
		},
		{
			name: "only vuln gate enabled and fails",
			config: &portagerv1alpha1.ValidationConfig{
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{
					Enabled:     true,
					MaxSeverity: "high",
				},
			},
			vulnErr:    errors.New("findings exceed threshold"),
			wantVerify: false,
			wantErr:    true,
		},
		{
			name: "sbom gate enabled and passes",
			config: &portagerv1alpha1.ValidationConfig{
				SbomGate: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			},
			wantVerify: true,
		},
		{
			name: "sbom gate enabled and fails",
			config: &portagerv1alpha1.ValidationConfig{
				SbomGate: &portagerv1alpha1.SbomGateConfig{Enabled: true},
			},
			sbomErr:    errors.New("no SBOM found"),
			wantVerify: false,
			wantErr:    true,
		},
		{
			name: "all gates pass",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign:            &portagerv1alpha1.CosignConfig{Enabled: true, PublicKey: "test-key"},
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{Enabled: true, MaxSeverity: "high"},
				SbomGate:          &portagerv1alpha1.SbomGateConfig{Enabled: true},
			},
			wantVerify: true,
		},
		{
			name: "cosign and vuln pass but sbom fails",
			config: &portagerv1alpha1.ValidationConfig{
				Cosign:            &portagerv1alpha1.CosignConfig{Enabled: true, PublicKey: "test-key"},
				VulnerabilityGate: &portagerv1alpha1.VulnerabilityGateConfig{Enabled: true, MaxSeverity: "high"},
				SbomGate:          &portagerv1alpha1.SbomGateConfig{Enabled: true},
			},
			sbomErr:    errors.New("no SBOM found"),
			wantVerify: false,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &Validator{
				CosignVerifier:       &mockCosignVerifier{err: tt.cosignErr},
				VulnerabilityChecker: &mockVulnChecker{err: tt.vulnErr},
				SbomChecker:          &mockSbomChecker{err: tt.sbomErr},
			}

			result := v.Validate(context.Background(), "fake-registry.invalid/test:latest", tt.config, authn.Anonymous)

			if result.Verified != tt.wantVerify {
				t.Errorf("Verified = %v, want %v", result.Verified, tt.wantVerify)
			}
			if tt.wantErr && result.Error == "" {
				t.Error("expected error message but got empty")
			}
			if !tt.wantErr && result.Error != "" {
				t.Errorf("expected no error but got: %s", result.Error)
			}
		})
	}
}
