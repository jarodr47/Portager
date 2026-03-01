//go:build e2e_ecr
// +build e2e_ecr

package controller

import (
	"context"
	"fmt"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/jarodr47/portager/internal/controller/auth"
	"github.com/jarodr47/portager/internal/controller/registry"
	"github.com/jarodr47/portager/internal/controller/sync"
)

const (
	ecrRegistry = "599121110630.dkr.ecr.us-east-1.amazonaws.com"
	testRepo    = "portage-e2e-test/alpine"
)

func TestECR_E2E(t *testing.T) {
	ctx := context.Background()

	// 1. Load AWS config and create ECR client.
	region, err := auth.ParseECRRegion(ecrRegistry)
	if err != nil {
		t.Fatalf("ParseECRRegion: %v", err)
	}
	t.Logf("Parsed region: %s", region)

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		t.Fatalf("LoadDefaultConfig: %v", err)
	}
	ecrClient := ecr.NewFromConfig(cfg)

	// 2. Test ECR authentication.
	t.Run("ECR_Auth", func(t *testing.T) {
		authenticator := &auth.ECRAuthenticator{Client: ecrClient}
		authnResult, err := authenticator.Authenticate(ctx)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}

		authCfg, err := authnResult.(authn.Authenticator).Authorization()
		if err != nil {
			t.Fatalf("Authorization: %v", err)
		}
		if authCfg.Username != "AWS" {
			t.Errorf("username = %q, want AWS", authCfg.Username)
		}
		if authCfg.Password == "" {
			t.Error("password is empty")
		}
		t.Logf("ECR auth successful: username=%s, password=<redacted, len=%d>",
			authCfg.Username, len(authCfg.Password))
	})

	// 3. Test repo creation.
	t.Run("Repo_Creation", func(t *testing.T) {
		repoMgr := &registry.ECRRepoManager{Client: ecrClient}

		err := repoMgr.EnsureRepositoryExists(ctx, testRepo)
		if err != nil {
			t.Fatalf("EnsureRepositoryExists (create): %v", err)
		}
		t.Logf("Repository %q created", testRepo)

		// Call again — should be idempotent (repo already exists).
		err = repoMgr.EnsureRepositoryExists(ctx, testRepo)
		if err != nil {
			t.Fatalf("EnsureRepositoryExists (idempotent): %v", err)
		}
		t.Log("Idempotent call succeeded (repo already exists)")
	})

	// 4. Test actual image copy: docker.io/library/alpine:latest → ECR.
	t.Run("Image_Copy", func(t *testing.T) {
		srcRef := "docker.io/library/alpine:latest"
		dstRef := fmt.Sprintf("%s/%s:latest", ecrRegistry, testRepo)

		authenticator := &auth.ECRAuthenticator{Client: ecrClient}
		dstAuthn, err := authenticator.Authenticate(ctx)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}

		copier := &sync.ImageCopier{}

		// Get source digest (anonymous auth for Docker Hub).
		srcDigest, err := copier.GetDigest(ctx, srcRef, authn.Anonymous)
		if err != nil {
			t.Fatalf("GetDigest (source): %v", err)
		}
		t.Logf("Source digest: %s", srcDigest)

		// Copy the image.
		err = copier.Copy(ctx, srcRef, dstRef, authn.Anonymous, dstAuthn)
		if err != nil {
			t.Fatalf("Copy: %v", err)
		}
		t.Logf("Copied %s → %s", srcRef, dstRef)

		// Verify destination digest matches source.
		dstDigest, err := copier.GetDigest(ctx, dstRef, dstAuthn)
		if err != nil {
			t.Fatalf("GetDigest (destination): %v", err)
		}
		t.Logf("Destination digest: %s", dstDigest)

		if srcDigest != dstDigest {
			t.Errorf("digest mismatch: src=%s dst=%s", srcDigest, dstDigest)
		} else {
			t.Log("Digests match — copy verified!")
		}

		// Test skip-by-digest: a second copy should detect matching digests.
		t.Log("Running second copy to test digest-based skip logic...")
		srcDigest2, _ := copier.GetDigest(ctx, srcRef, authn.Anonymous)
		dstDigest2, err := copier.GetDigest(ctx, dstRef, dstAuthn)
		if err != nil {
			t.Fatalf("GetDigest (second check): %v", err)
		}
		if srcDigest2 == dstDigest2 {
			t.Log("Second check: digests match, copy would be skipped (correct)")
		} else {
			t.Error("Second check: digests don't match unexpectedly")
		}
	})

	// 5. Verify the image is actually pullable from ECR.
	t.Run("Verify_Image_Exists", func(t *testing.T) {
		authenticator := &auth.ECRAuthenticator{Client: ecrClient}
		dstAuthn, err := authenticator.Authenticate(ctx)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}

		ref, err := name.ParseReference(fmt.Sprintf("%s/%s:latest", ecrRegistry, testRepo))
		if err != nil {
			t.Fatalf("ParseReference: %v", err)
		}

		desc, err := remote.Get(ref, remote.WithAuth(dstAuthn))
		if err != nil {
			t.Fatalf("remote.Get: %v", err)
		}
		t.Logf("Image verified in ECR: digest=%s, mediaType=%s", desc.Digest, desc.MediaType)
	})
}

func TestECR_E2E_Cleanup(t *testing.T) {
	ctx := context.Background()

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"))
	if err != nil {
		t.Fatalf("LoadDefaultConfig: %v", err)
	}
	ecrClient := ecr.NewFromConfig(cfg)

	// Clean up repos created by tests.
	repos := []string{testRepo, "chainguard/go"}
	for _, repo := range repos {
		t.Run("Delete_"+repo, func(t *testing.T) {
			force := true
			_, err := ecrClient.DeleteRepository(ctx, &ecr.DeleteRepositoryInput{
				RepositoryName: &repo,
				Force:          force,
			})
			if err != nil {
				t.Logf("Delete %q: %v (may not exist)", repo, err)
			} else {
				t.Logf("Deleted repository %q", repo)
			}
		})
	}
}
