package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/google/go-containerregistry/pkg/authn"
)

// acrUsername is the fixed username used for ACR refresh token authentication.
// Azure requires this specific GUID as the username when authenticating with
// an ACR refresh token obtained via OAuth2 token exchange.
const acrUsername = "00000000-0000-0000-0000-000000000000"

// acrScope is the AAD scope used to obtain an access token for ACR.
// Individual ACR instances are not registered as AAD resource principals,
// so we must use the global Azure management scope. The returned token is
// then exchanged for an ACR-specific refresh token at /oauth2/exchange.
const acrScope = "https://management.azure.com/.default"

// ACRTokenClient abstracts the ACR OAuth2 token exchange endpoint.
// This allows unit testing without real HTTP calls.
type ACRTokenClient interface {
	// ExchangeAADToken exchanges an AAD access token for an ACR refresh token.
	ExchangeAADToken(ctx context.Context, registry, aadToken string) (string, error)
}

// ACRAuthenticator uses Azure Workload Identity (or any azcore.TokenCredential)
// to authenticate to Azure Container Registry.
type ACRAuthenticator struct {
	TokenClient ACRTokenClient
	Credential  azcore.TokenCredential
	Registry    string
}

// Authenticate obtains an AAD access token scoped to the ACR instance, exchanges
// it for an ACR refresh token via the registry's OAuth2 endpoint, and returns
// an authn.Authenticator usable by go-containerregistry.
func (a *ACRAuthenticator) Authenticate(ctx context.Context) (authn.Authenticator, error) {
	// Request an AAD token scoped to Azure management plane.
	// Individual ACR instances are not AAD resource principals, so we use
	// the global management scope and exchange it for an ACR refresh token.
	token, err := a.Credential.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{acrScope},
	})
	if err != nil {
		return nil, fmt.Errorf("acquiring AAD token for ACR: %w", err)
	}

	// Exchange the AAD token for an ACR refresh token.
	refreshToken, err := a.TokenClient.ExchangeAADToken(ctx, a.Registry, token.Token)
	if err != nil {
		return nil, fmt.Errorf("exchanging AAD token for ACR refresh token: %w", err)
	}

	return authn.FromConfig(authn.AuthConfig{
		Username: acrUsername,
		Password: refreshToken,
	}), nil
}

// defaultACRTokenClient exchanges AAD tokens for ACR refresh tokens via HTTP.
type defaultACRTokenClient struct {
	httpClient *http.Client
}

// NewACRTokenClient returns the default ACRTokenClient that calls the real
// ACR OAuth2 exchange endpoint.
func NewACRTokenClient() ACRTokenClient {
	return &defaultACRTokenClient{
		httpClient: &http.Client{},
	}
}

// exchangeResponse is the JSON response from ACR's /oauth2/exchange endpoint.
type exchangeResponse struct {
	RefreshToken string `json:"refresh_token"`
}

// ExchangeAADToken posts to the ACR /oauth2/exchange endpoint to convert an
// AAD access token into an ACR refresh token.
func (c *defaultACRTokenClient) ExchangeAADToken(ctx context.Context, registry, aadToken string) (string, error) {
	exchangeURL := fmt.Sprintf("https://%s/oauth2/exchange", registry)

	form := url.Values{
		"grant_type":   {"access_token"},
		"service":      {registry},
		"access_token": {aadToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating ACR exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling ACR exchange endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ACR exchange endpoint returned status %d", resp.StatusCode)
	}

	var result exchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding ACR exchange response: %w", err)
	}

	if result.RefreshToken == "" {
		return "", fmt.Errorf("ACR exchange returned empty refresh token")
	}

	return result.RefreshToken, nil
}

// IsACRRegistry returns true if the given hostname is an Azure Container Registry.
func IsACRRegistry(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".azurecr.io")
}
