package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"golang.org/x/oauth2/google"
)

// garScope is the OAuth2 scope required for Artifact Registry read/write operations.
const garScope = "https://www.googleapis.com/auth/devstorage.read_write"

// GARAuthenticator uses Application Default Credentials (ADC) and GKE Workload
// Identity to authenticate with Google Artifact Registry.
//
// In GKE, the pod's Kubernetes service account must be annotated to bind it to
// a GCP service account that has the Artifact Registry Reader or Writer role.
// The controller fetches a fresh token on every reconcile via the GKE metadata
// server — no token caching is needed.
type GARAuthenticator struct {
	// Registry is the GAR registry field from the ImageSync spec.
	// May be just the hostname ("us-central1-docker.pkg.dev") or include a
	// path prefix ("us-central1-docker.pkg.dev/my-project"). Only the hostname
	// portion is used for diagnostic messages.
	Registry string
}

// Authenticate resolves credentials for GAR using Application Default Credentials.
// On GKE with Workload Identity, ADC resolves via the instance metadata server.
// Outside GKE, ADC falls back to GOOGLE_APPLICATION_CREDENTIALS or gcloud credentials.
//
// Credentials are returned as Basic auth with username "oauth2accesstoken", which
// is the standard Docker-compatible credential format for Google registries.
func (g *GARAuthenticator) Authenticate(ctx context.Context) (authn.Authenticator, error) {
	ts, err := google.DefaultTokenSource(ctx, garScope)
	if err != nil {
		return nil, fmt.Errorf("getting GAR token source for %q: %w", garHost(g.Registry), err)
	}
	token, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("fetching GAR access token for %q: %w", garHost(g.Registry), err)
	}
	return authn.FromConfig(authn.AuthConfig{
		Username: "oauth2accesstoken",
		Password: token.AccessToken,
	}), nil
}

// garHost extracts the hostname from a registry field that may include a path prefix.
//
//	"us-central1-docker.pkg.dev"                    → "us-central1-docker.pkg.dev"
//	"us-central1-docker.pkg.dev/my-project/my-repo" → "us-central1-docker.pkg.dev"

func garHost(registry string) string {
	host, _, _ := strings.Cut(registry, "/")
	return host
}
