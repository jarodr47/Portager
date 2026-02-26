package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// ImageCopier handles copying container images between registries
// using go-containerregistry's crane library.
type ImageCopier struct{}

// Copy copies a single image from src to dst.
//
// src and dst are full image references like "docker.io/library/alpine:latest".
// srcAuth and dstAuth are the resolved credentials for each registry.
//
// crane.Copy handles both single manifests and manifest lists (multi-arch)
// transparently — you don't need to know which type the image is.
func (c *ImageCopier) Copy(ctx context.Context, src, dst string, srcAuth, dstAuth authn.Authenticator) error {
	log := ctrllog.FromContext(ctx)
	log.Info("Copying image", "source", src, "destination", dst)

	// Build crane options.
	// crane.WithContext passes the context so cancellation works.
	// The keychain resolves credentials per-registry during the copy.
	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(&staticKeychain{
			src:     src,
			dst:     dst,
			srcAuth: srcAuth,
			dstAuth: dstAuth,
		}),
	}

	// Local registries (localhost, 127.0.0.1) run on HTTP, not HTTPS.
	// crane defaults to HTTPS, so we need to tell it to use plain HTTP.
	if isInsecureRegistry(dst) {
		opts = append(opts, crane.Insecure)
	}

	if err := crane.Copy(src, dst, opts...); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}

	log.Info("Successfully copied image", "source", src, "destination", dst)
	return nil
}

// staticKeychain implements authn.Keychain by mapping specific image
// references to their authenticators. When crane.Copy needs credentials
// for a registry, it calls Resolve() with the registry info.
type staticKeychain struct {
	src, dst         string
	srcAuth, dstAuth authn.Authenticator
}

// Resolve is called by crane to get credentials for a specific registry.
// It checks if the registry matches the source or destination and returns
// the appropriate authenticator. Falls back to anonymous for unknown registries.
func (k *staticKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	registry := resource.RegistryStr()

	// Parse the source and destination to extract their registry hostnames.
	srcRef, err := name.ParseReference(k.src)
	if err == nil && srcRef.Context().RegistryStr() == registry {
		return k.srcAuth, nil
	}

	dstRef, err := name.ParseReference(k.dst)
	if err == nil && dstRef.Context().RegistryStr() == registry {
		return k.dstAuth, nil
	}

	return authn.Anonymous, nil
}

// GetDigest retrieves the manifest digest of an image without downloading
// its layers. This uses an HTTP HEAD request — fast and bandwidth-efficient.
//
// Returns the digest string (e.g., "sha256:abc123...") or an error if the
// image doesn't exist or the registry is unreachable.
//
// Unlike Copy(), GetDigest talks to a single registry, so we use
// crane.WithAuth(auth) directly instead of the dual-registry keychain.
func (c *ImageCopier) GetDigest(ctx context.Context, ref string, auth authn.Authenticator) (string, error) {
	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuth(auth),
	}
	if isInsecureRegistry(ref) {
		opts = append(opts, crane.Insecure)
	}

	digest, err := crane.Digest(ref, opts...)
	if err != nil {
		return "", fmt.Errorf("getting digest for %s: %w", ref, err)
	}
	return digest, nil
}

// isInsecureRegistry returns true for registries that use HTTP (not HTTPS).
// In practice, this means localhost and 127.0.0.1 development registries.
func isInsecureRegistry(ref string) bool {
	return strings.HasPrefix(ref, "localhost") ||
		strings.HasPrefix(ref, "localhost:") ||
		strings.HasPrefix(ref, "127.0.0.1")
}
