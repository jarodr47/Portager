package registry

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

type mockECRRepoClient struct {
	describeErr error
	createErr   error
	created     []string // tracks repo names passed to CreateRepository
}

func (m *mockECRRepoClient) DescribeRepositories(_ context.Context, _ *ecr.DescribeRepositoriesInput, _ ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error) {
	if m.describeErr != nil {
		return nil, m.describeErr
	}
	return &ecr.DescribeRepositoriesOutput{}, nil
}

func (m *mockECRRepoClient) CreateRepository(_ context.Context, input *ecr.CreateRepositoryInput, _ ...func(*ecr.Options)) (*ecr.CreateRepositoryOutput, error) {
	if input.RepositoryName != nil {
		m.created = append(m.created, *input.RepositoryName)
	}
	if m.createErr != nil {
		return nil, m.createErr
	}
	return &ecr.CreateRepositoryOutput{}, nil
}

func TestECRRepoManager_EnsureRepositoryExists(t *testing.T) {
	tests := []struct {
		name        string
		describeErr error
		createErr   error
		wantErr     string
		wantCreated bool
	}{
		{
			name:        "repo already exists",
			wantCreated: false,
		},
		{
			name:        "repo missing, create succeeds",
			describeErr: &ecrtypes.RepositoryNotFoundException{Message: strPtr("not found")},
			wantCreated: true,
		},
		{
			name:        "repo missing, create fails",
			describeErr: &ecrtypes.RepositoryNotFoundException{Message: strPtr("not found")},
			createErr:   fmt.Errorf("permission denied"),
			wantErr:     "creating ECR repository",
		},
		{
			name:        "unexpected describe error",
			describeErr: fmt.Errorf("throttled"),
			wantErr:     "checking ECR repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockECRRepoClient{
				describeErr: tt.describeErr,
				createErr:   tt.createErr,
			}
			mgr := &ECRRepoManager{Client: mock}

			err := mgr.EnsureRepositoryExists(context.Background(), "my-repo")

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsSubstring(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantCreated && len(mock.created) == 0 {
				t.Error("expected CreateRepository to be called, but it wasn't")
			}
			if !tt.wantCreated && len(mock.created) > 0 {
				t.Errorf("CreateRepository called unexpectedly with %v", mock.created)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
