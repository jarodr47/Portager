package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// ECRRepoClient is the subset of the ECR API needed for repository management.
type ECRRepoClient interface {
	DescribeRepositories(ctx context.Context, params *ecr.DescribeRepositoriesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error)
	CreateRepository(ctx context.Context, params *ecr.CreateRepositoryInput, optFns ...func(*ecr.Options)) (*ecr.CreateRepositoryOutput, error)
}

// ECRRepoManager creates ECR repositories on demand.
type ECRRepoManager struct {
	Client ECRRepoClient
}

// EnsureRepositoryExists checks if an ECR repository exists and creates it
// if it doesn't. Uses MUTABLE image tag mutability since Portage re-pushes
// tags like "latest".
func (m *ECRRepoManager) EnsureRepositoryExists(ctx context.Context, repoName string) error {
	_, err := m.Client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: []string{repoName},
	})
	if err == nil {
		return nil // already exists
	}

	var notFound *ecrtypes.RepositoryNotFoundException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("checking ECR repository %q: %w", repoName, err)
	}

	_, err = m.Client.CreateRepository(ctx, &ecr.CreateRepositoryInput{
		RepositoryName:     &repoName,
		ImageTagMutability: ecrtypes.ImageTagMutabilityMutable,
	})
	if err != nil {
		return fmt.Errorf("creating ECR repository %q: %w", repoName, err)
	}

	return nil
}
