package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/google/go-containerregistry/pkg/authn"
)

// mockECRClient implements ECRClient for testing.
type mockECRClient struct {
	output *ecr.GetAuthorizationTokenOutput
	err    error
}

func (m *mockECRClient) GetAuthorizationToken(_ context.Context, _ *ecr.GetAuthorizationTokenInput, _ ...func(*ecr.Options)) (*ecr.GetAuthorizationTokenOutput, error) {
	return m.output, m.err
}

func TestECRAuthenticator_Authenticate(t *testing.T) {
	validToken := base64.StdEncoding.EncodeToString([]byte("AWS:my-secret-password"))

	tests := []struct {
		name    string
		client  ECRClient
		wantErr string
	}{
		{
			name: "successful auth",
			client: &mockECRClient{
				output: &ecr.GetAuthorizationTokenOutput{
					AuthorizationData: []ecrtypes.AuthorizationData{
						{AuthorizationToken: &validToken},
					},
				},
			},
		},
		{
			name: "AWS API error",
			client: &mockECRClient{
				err: fmt.Errorf("access denied"),
			},
			wantErr: "ECR GetAuthorizationToken: access denied",
		},
		{
			name: "empty authorization data",
			client: &mockECRClient{
				output: &ecr.GetAuthorizationTokenOutput{
					AuthorizationData: []ecrtypes.AuthorizationData{},
				},
			},
			wantErr: "ECR returned empty authorization data",
		},
		{
			name: "nil token",
			client: &mockECRClient{
				output: &ecr.GetAuthorizationTokenOutput{
					AuthorizationData: []ecrtypes.AuthorizationData{
						{AuthorizationToken: nil},
					},
				},
			},
			wantErr: "ECR authorization token is nil",
		},
		{
			name: "invalid base64",
			client: &mockECRClient{
				output: func() *ecr.GetAuthorizationTokenOutput {
					bad := "not-valid-base64!@#$"
					return &ecr.GetAuthorizationTokenOutput{
						AuthorizationData: []ecrtypes.AuthorizationData{
							{AuthorizationToken: &bad},
						},
					}
				}(),
			},
			wantErr: "decoding ECR token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ECRAuthenticator{Client: tt.client}
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
			cfg, err := result.(authn.Authenticator).Authorization()
			if err != nil {
				t.Fatalf("Authorization() error: %v", err)
			}
			if cfg.Username != "AWS" {
				t.Errorf("username = %q, want %q", cfg.Username, "AWS")
			}
			if cfg.Password != "my-secret-password" {
				t.Errorf("password = %q, want %q", cfg.Password, "my-secret-password")
			}
		})
	}
}

func TestParseECRRegion(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		wantRegion string
		wantErr    bool
	}{
		{
			name:       "commercial region",
			host:       "123456789012.dkr.ecr.us-east-1.amazonaws.com",
			wantRegion: "us-east-1",
		},
		{
			name:       "govcloud region",
			host:       "123456789012.dkr.ecr.us-gov-west-1.amazonaws.com",
			wantRegion: "us-gov-west-1",
		},
		{
			name:       "eu region",
			host:       "999888777666.dkr.ecr.eu-west-1.amazonaws.com",
			wantRegion: "eu-west-1",
		},
		{
			name:    "not an ECR hostname",
			host:    "docker.io",
			wantErr: true,
		},
		{
			name:    "empty string",
			host:    "",
			wantErr: true,
		},
		{
			name:    "partial ECR hostname",
			host:    "dkr.ecr.us-east-1.amazonaws.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, err := ParseECRRegion(tt.host)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if region != tt.wantRegion {
				t.Errorf("region = %q, want %q", region, tt.wantRegion)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
