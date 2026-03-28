package verify

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

// ValidationResult holds the outcome of pre-sync validation.
type ValidationResult struct {
	Verified bool
	Error    string // empty on success
}

// CosignVerifier verifies cosign signatures on container images.
type CosignVerifier interface {
	VerifySignature(ctx context.Context, imageRef string, config *portagerv1alpha1.CosignConfig) error
}

// VulnerabilityChecker checks OCI attestation SARIF reports for severity violations.
type VulnerabilityChecker interface {
	CheckVulnerabilities(ctx context.Context, imageRef string, config *portagerv1alpha1.VulnerabilityGateConfig, auth authn.Authenticator) error
}

// SbomChecker checks that an SBOM (SPDX or CycloneDX) is attached as an OCI referrer.
type SbomChecker interface {
	CheckSbom(ctx context.Context, imageRef string, config *portagerv1alpha1.SbomGateConfig, auth authn.Authenticator) error
}

// Validator runs all configured validation gates for a single image reference.
type Validator struct {
	CosignVerifier       CosignVerifier
	VulnerabilityChecker VulnerabilityChecker
	SbomChecker          SbomChecker
}

// Validate runs all enabled validation gates. Returns on first failure.
func (v *Validator) Validate(ctx context.Context, imageRef string, config *portagerv1alpha1.ValidationConfig, srcAuth authn.Authenticator) ValidationResult {
	if config == nil {
		return ValidationResult{Verified: true}
	}

	if config.Cosign != nil && config.Cosign.Enabled {
		if err := v.CosignVerifier.VerifySignature(ctx, imageRef, config.Cosign); err != nil {
			return ValidationResult{
				Verified: false,
				Error:    fmt.Sprintf("cosign verification failed: %v", err),
			}
		}
	}

	if config.VulnerabilityGate != nil && config.VulnerabilityGate.Enabled {
		if err := v.VulnerabilityChecker.CheckVulnerabilities(ctx, imageRef, config.VulnerabilityGate, srcAuth); err != nil {
			return ValidationResult{
				Verified: false,
				Error:    fmt.Sprintf("vulnerability gate failed: %v", err),
			}
		}
	}

	if config.SbomGate != nil && config.SbomGate.Enabled {
		if err := v.SbomChecker.CheckSbom(ctx, imageRef, config.SbomGate, srcAuth); err != nil {
			return ValidationResult{
				Verified: false,
				Error:    fmt.Sprintf("SBOM gate failed: %v", err),
			}
		}
	}

	return ValidationResult{Verified: true}
}
