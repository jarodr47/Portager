package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/google/go-containerregistry/pkg/authn"
)

// ecrHostPattern matches ECR registry hostnames and captures the region.
// Format: {account_id}.dkr.ecr.{region}.amazonaws.com
// Also handles GovCloud: {account_id}.dkr.ecr.{region}.amazonaws.com
var ecrHostPattern = regexp.MustCompile(`^\d+\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com$`)

// ECRClient is the subset of the ECR API needed for authentication.
// Using an interface allows unit testing with mocks.
type ECRClient interface {
	GetAuthorizationToken(ctx context.Context, params *ecr.GetAuthorizationTokenInput, optFns ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error)
}

// ECRAuthenticator uses the ECR GetAuthorizationToken API (via IRSA) to
// obtain registry credentials.
type ECRAuthenticator struct {
	Client ECRClient
}

// Authenticate calls ECR GetAuthorizationToken, decodes the base64 token
// (format "AWS:<password>"), and returns an authn.Authenticator.
func (e *ECRAuthenticator) Authenticate(ctx context.Context) (authn.Authenticator, error) {
	out, err := e.Client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return nil, fmt.Errorf("ECR GetAuthorizationToken: %w", err)
	}

	if len(out.AuthorizationData) == 0 {
		return nil, fmt.Errorf("ECR returned empty authorization data")
	}

	token := out.AuthorizationData[0].AuthorizationToken
	if token == nil {
		return nil, fmt.Errorf("ECR authorization token is nil")
	}

	decoded, err := base64.StdEncoding.DecodeString(*token)
	if err != nil {
		return nil, fmt.Errorf("decoding ECR token: %w", err)
	}

	// Token format is "AWS:<password>"
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected ECR token format")
	}

	return authn.FromConfig(authn.AuthConfig{
		Username: parts[0],
		Password: parts[1],
	}), nil
}

// ParseECRRegion extracts the AWS region from an ECR registry hostname.
// Returns an error if the hostname doesn't match the ECR pattern.
func ParseECRRegion(registryHost string) (string, error) {
	matches := ecrHostPattern.FindStringSubmatch(registryHost)
	if matches == nil {
		return "", fmt.Errorf("not a valid ECR hostname: %q", registryHost)
	}
	return matches[1], nil
}
