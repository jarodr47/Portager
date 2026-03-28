package verify

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

const (
	spdxArtifactType      = "application/spdx+json"
	cyclonedxArtifactType = "application/vnd.cyclonedx+json"
)

// sbomChecker is the production implementation.
type sbomChecker struct {
	fetcher ReferrersFetcher
}

// NewSbomChecker returns a production SbomChecker.
func NewSbomChecker() SbomChecker {
	return &sbomChecker{
		fetcher: &ociReferrersFetcher{},
	}
}

func (c *sbomChecker) CheckSbom(ctx context.Context, imageRef string, config *portagerv1alpha1.SbomGateConfig, auth authn.Authenticator) error {
	if config == nil || !config.Enabled {
		return nil
	}

	referrers, err := c.fetcher.FetchReferrers(ctx, imageRef, auth)
	if err != nil {
		return fmt.Errorf("failed to fetch referrers for %s: %w", imageRef, err)
	}

	for _, r := range referrers {
		if r.ArtifactType == spdxArtifactType || r.ArtifactType == cyclonedxArtifactType {
			return nil
		}
	}

	return fmt.Errorf("no SBOM found for %s; an SPDX or CycloneDX SBOM must be attached as an OCI referrer", imageRef)
}
