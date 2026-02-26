package auth

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
)

// AnonymousAuthenticator returns anonymous credentials.
// Use for public source registries (e.g., Docker Hub for public images)
// or local test registries with no authentication configured.
type AnonymousAuthenticator struct{}

// Authenticate returns authn.Anonymous — no credentials attached to requests.
func (a *AnonymousAuthenticator) Authenticate(ctx context.Context) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}
