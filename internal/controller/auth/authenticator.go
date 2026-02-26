package auth

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
)

// Authenticator resolves credentials for a container registry.
//
// Each implementation handles a different credential source:
//   - AnonymousAuthenticator: no credentials (public registries)
//   - SecretAuthenticator: reads a Kubernetes dockerconfigjson secret
//   - ECRAuthenticator: uses IRSA to get ECR tokens (Phase 4)
//
// The returned authn.Authenticator is what go-containerregistry uses
// to attach credentials to registry HTTP requests.
type Authenticator interface {
	Authenticate(ctx context.Context) (authn.Authenticator, error)
}
