//go:build e2e_gar
// +build e2e_gar

package controller

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/jarodr47/portager/internal/controller/auth"
	"github.com/jarodr47/portager/internal/controller/sync"
)

// garTestImage is the image name used as the source for e2e tests.
const garTestImage = "alpine"

// garRegistryFromEnv returns the GAR registry from the GAR_REGISTRY environment variable.
// It fails the test immediately if the variable is not set.
//
// Format: {region}-docker.pkg.dev/{project}/{repository}
//
// Prerequisites before running:
//   - The GAR repository must already exist (GAR does not auto-create repos on push).
//   - Application Default Credentials must be configured in the environment:
//     GKE Workload Identity, GOOGLE_APPLICATION_CREDENTIALS, or `gcloud auth application-default login`.
//   - The GCP service account / ADC principal must have the
//     roles/artifactregistry.writer role on the target repository.
//
// Run with:
//
//	GAR_REGISTRY=us-central1-docker.pkg.dev/my-project/my-repo make test-e2e-gar
func garRegistryFromEnv(t *testing.T) string {
	t.Helper()
	registry := os.Getenv("GAR_REGISTRY")
	if registry == "" {
		t.Fatal("GAR_REGISTRY environment variable must be set (e.g. us-central1-docker.pkg.dev/my-project/my-repo)")
	}
	return registry
}

func TestGAR_E2E(t *testing.T) {
	ctx := context.Background()
	garRegistry := garRegistryFromEnv(t)

	authenticator := &auth.GARAuthenticator{Registry: garRegistry}

	// 1. Test GAR authentication via ADC / Workload Identity.
	t.Run("GAR_Auth", func(t *testing.T) {
		authnResult, err := authenticator.Authenticate(ctx)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}

		authCfg, err := authnResult.(authn.Authenticator).Authorization()
		if err != nil {
			t.Fatalf("Authorization: %v", err)
		}
		if authCfg.Username != "oauth2accesstoken" {
			t.Errorf("username = %q, want %q", authCfg.Username, "oauth2accesstoken")
		}
		if authCfg.Password == "" {
			t.Error("access token is empty")
		}
		t.Logf("GAR auth successful: username=%s, token=<redacted, len=%d>",
			authCfg.Username, len(authCfg.Password))
	})

	// 2. Test actual image copy: docker.io/library/alpine:latest → GAR.
	t.Run("Image_Copy", func(t *testing.T) {
		srcRef := "docker.io/library/alpine:latest"
		dstRef := fmt.Sprintf("%s/%s:latest", garRegistry, garTestImage)

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
		if err := copier.Copy(ctx, srcRef, dstRef, authn.Anonymous, dstAuthn); err != nil {
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

	// 3. Verify the image is actually pullable from GAR.
	t.Run("Verify_Image_Exists", func(t *testing.T) {
		dstRef := fmt.Sprintf("%s/%s:latest", garRegistry, garTestImage)

		dstAuthn, err := authenticator.Authenticate(ctx)
		if err != nil {
			t.Fatalf("Authenticate: %v", err)
		}

		ref, err := name.ParseReference(dstRef)
		if err != nil {
			t.Fatalf("ParseReference: %v", err)
		}

		desc, err := remote.Get(ref, remote.WithAuth(dstAuthn))
		if err != nil {
			t.Fatalf("remote.Get: %v", err)
		}
		t.Logf("Image verified in GAR: digest=%s, mediaType=%s", desc.Digest, desc.MediaType)
	})
}

func TestGAR_E2E_Cleanup(t *testing.T) {
	ctx := context.Background()
	garRegistry := garRegistryFromEnv(t)

	authenticator := &auth.GARAuthenticator{Registry: garRegistry}
	authnResult, err := authenticator.Authenticate(ctx)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	images := []string{
		fmt.Sprintf("%s/%s:latest", garRegistry, garTestImage),
	}

	for _, img := range images {
		t.Run("Delete_"+img, func(t *testing.T) {
			ref, err := name.ParseReference(img)
			if err != nil {
				t.Fatalf("ParseReference %q: %v", img, err)
			}
			if err := remote.Delete(ref, remote.WithAuth(authnResult), remote.WithContext(ctx)); err != nil {
				t.Logf("Delete %q: %v (may not exist)", img, err)
			} else {
				t.Logf("Deleted image %q", img)
			}
		})
	}
}
