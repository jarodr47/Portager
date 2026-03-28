package verify

import (
	"context"
	"crypto"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v2/pkg/cosign"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/fulcioroots"
	"github.com/sigstore/sigstore/pkg/signature"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

// cosignVerifier is the production implementation using sigstore/cosign.
type cosignVerifier struct{}

// NewCosignVerifier returns a production CosignVerifier.
func NewCosignVerifier() CosignVerifier {
	return &cosignVerifier{}
}

func (v *cosignVerifier) VerifySignature(ctx context.Context, imageRef string, config *portagerv1alpha1.CosignConfig) error {
	if config == nil || !config.Enabled {
		return nil
	}

	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("invalid image reference %q: %w", imageRef, err)
	}

	co := &cosign.CheckOpts{
		IgnoreSCT:  true,
		IgnoreTlog: true,
	}

	switch {
	case config.PublicKey != "":
		// Key-based verification
		pubKey, err := cryptoutils.UnmarshalPEMToPublicKey([]byte(config.PublicKey))
		if err != nil {
			return fmt.Errorf("failed to parse public key: %w", err)
		}
		verifier, err := signature.LoadVerifier(pubKey, crypto.SHA256)
		if err != nil {
			return fmt.Errorf("failed to load verifier: %w", err)
		}
		co.SigVerifier = verifier

	case config.KeylessIssuer != "":
		// Keyless verification via Fulcio
		// Ensure TUF_ROOT points to a writable path for TUF metadata cache.
		// Distroless containers have a read-only $HOME, so default to /tmp/.sigstore.
		if os.Getenv("TUF_ROOT") == "" {
			if err := os.Setenv("TUF_ROOT", "/tmp/.sigstore"); err != nil {
				return fmt.Errorf("failed to set TUF_ROOT: %w", err)
			}
		}
		roots, err := fulcioroots.Get()
		if err != nil {
			return fmt.Errorf("failed to get Fulcio root certs: %w", err)
		}
		co.RootCerts = roots

		intermediates, err := fulcioroots.GetIntermediates()
		if err != nil {
			return fmt.Errorf("failed to get Fulcio intermediate certs: %w", err)
		}
		co.IntermediateCerts = intermediates

		co.Identities = []cosign.Identity{
			{Issuer: config.KeylessIssuer},
		}
		// Keyless requires transparency log verification
		co.IgnoreTlog = false

		rekorPubKeys, err := cosign.GetRekorPubs(ctx)
		if err != nil {
			return fmt.Errorf("failed to get Rekor public keys: %w", err)
		}
		co.RekorPubKeys = rekorPubKeys

		ctPubKeys, err := cosign.GetCTLogPubs(ctx)
		if err != nil {
			return fmt.Errorf("failed to get CT log public keys: %w", err)
		}
		co.CTLogPubKeys = ctPubKeys
		co.IgnoreSCT = false

	default:
		return fmt.Errorf("cosign verification enabled but neither publicKey nor keylessIssuer is configured")
	}

	sigs, _, err := cosign.VerifyImageSignatures(ctx, ref, co)
	if err != nil {
		return fmt.Errorf("cosign verification failed for %s: %w", imageRef, err)
	}
	if len(sigs) == 0 {
		return fmt.Errorf("no valid signatures found for %s", imageRef)
	}

	return nil
}
