package auth

import (
	"context"
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// mockTokenCredential implements azcore.TokenCredential for testing.
type mockTokenCredential struct {
	token string
	err   error
}

func (m *mockTokenCredential) GetToken(_ context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if m.err != nil {
		return azcore.AccessToken{}, m.err
	}
	return azcore.AccessToken{Token: m.token}, nil
}

// mockACRTokenClient implements ACRTokenClient for testing.
type mockACRTokenClient struct {
	refreshToken string
	err          error
}

func (m *mockACRTokenClient) ExchangeAADToken(_ context.Context, _, _ string) (string, error) {
	return m.refreshToken, m.err
}

func TestACRAuthenticator_Authenticate(t *testing.T) {
	tests := []struct {
		name       string
		credential azcore.TokenCredential
		client     ACRTokenClient
		wantErr    string
	}{
		{
			name:       "successful auth",
			credential: &mockTokenCredential{token: "aad-access-token"},
			client:     &mockACRTokenClient{refreshToken: "acr-refresh-token"},
		},
		{
			name:       "AAD token acquisition failure",
			credential: &mockTokenCredential{err: fmt.Errorf("workload identity not configured")},
			client:     &mockACRTokenClient{},
			wantErr:    "acquiring AAD token for ACR",
		},
		{
			name:       "ACR exchange endpoint failure",
			credential: &mockTokenCredential{token: "aad-access-token"},
			client:     &mockACRTokenClient{err: fmt.Errorf("exchange failed: 401")},
			wantErr:    "exchanging AAD token for ACR refresh token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ACRAuthenticator{
				TokenClient: tt.client,
				Credential:  tt.credential,
				Registry:    "myregistry.azurecr.io",
			}
			result, err := a.Authenticate(context.Background())

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if got := err.Error(); !contains(got, tt.wantErr) {
					t.Fatalf("error %q does not contain %q", got, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify the returned authenticator has the right credentials.
			cfg, err := result.Authorization()
			if err != nil {
				t.Fatalf("Authorization() error: %v", err)
			}
			if cfg.Username != acrUsername {
				t.Errorf("username = %q, want %q", cfg.Username, acrUsername)
			}
			if cfg.Password != "acr-refresh-token" {
				t.Errorf("password = %q, want %q", cfg.Password, "acr-refresh-token")
			}
		})
	}
}

func TestIsACRRegistry(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{
			name: "standard ACR registry",
			host: "myregistry.azurecr.io",
			want: true,
		},
		{
			name: "ACR with subdomain",
			host: "myorg.myregistry.azurecr.io",
			want: true,
		},
		{
			name: "ACR uppercase",
			host: "MyRegistry.AzureCR.IO",
			want: true,
		},
		{
			name: "ECR registry",
			host: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			want: false,
		},
		{
			name: "Docker Hub",
			host: "docker.io",
			want: false,
		},
		{
			name: "empty string",
			host: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsACRRegistry(tt.host)
			if got != tt.want {
				t.Errorf("IsACRRegistry(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}
