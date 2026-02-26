package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SecretAuthenticator reads a kubernetes.io/dockerconfigjson Secret and
// extracts credentials for a specific registry.
type SecretAuthenticator struct {
	// Client is the Kubernetes client used to read the Secret.
	Client client.Client

	// SecretKey identifies the Secret by name and namespace.
	SecretKey types.NamespacedName

	// Registry is the hostname to look up in the Docker config's "auths" map.
	// Must match exactly what's in the Secret (e.g., "cgr.dev", "docker.io").
	Registry string
}

// dockerConfigJSON mirrors the structure of ~/.docker/config.json.
// The "auths" field maps registry hostnames to credentials.
type dockerConfigJSON struct {
	Auths map[string]dockerConfigEntry `json:"auths"`
}

// dockerConfigEntry holds credentials for a single registry.
type dockerConfigEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Auth     string `json:"auth"` // base64(username:password)
}

// Authenticate reads the referenced Secret, parses the Docker config JSON,
// and returns an authn.Authenticator with credentials for the target registry.
func (s *SecretAuthenticator) Authenticate(ctx context.Context) (authn.Authenticator, error) {
	// 1. Fetch the secret from Kubernetes
	var secret corev1.Secret
	if err := s.Client.Get(ctx, s.SecretKey, &secret); err != nil {
		return nil, fmt.Errorf("reading auth secret %s/%s: %w",
			s.SecretKey.Namespace, s.SecretKey.Name, err)
	}

	// 2. Validate the secret type
	if secret.Type != corev1.SecretTypeDockerConfigJson {
		return nil, fmt.Errorf("secret %s/%s has type %s, expected %s",
			s.SecretKey.Namespace, s.SecretKey.Name,
			secret.Type, corev1.SecretTypeDockerConfigJson)
	}

	// 3. Extract and parse the .dockerconfigjson data
	configBytes, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return nil, fmt.Errorf("secret %s/%s missing key %s",
			s.SecretKey.Namespace, s.SecretKey.Name, corev1.DockerConfigJsonKey)
	}

	var config dockerConfigJSON
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return nil, fmt.Errorf("parsing docker config from secret %s/%s: %w",
			s.SecretKey.Namespace, s.SecretKey.Name, err)
	}

	// 4. Look up credentials for the target registry.
	//    The two-value map lookup (val, ok) lets us distinguish
	//    "key not found" from "key exists with empty value".
	entry, ok := config.Auths[s.Registry]
	if !ok {
		return nil, fmt.Errorf("no credentials for registry %q in secret %s/%s",
			s.Registry, s.SecretKey.Namespace, s.SecretKey.Name)
	}

	// 5. Return an authn.Authenticator that go-containerregistry can use.
	//    authn.FromConfig converts username/password/auth into the right
	//    HTTP Authorization header for registry requests.
	return authn.FromConfig(authn.AuthConfig{
		Username: entry.Username,
		Password: entry.Password,
		Auth:     entry.Auth,
	}), nil
}
