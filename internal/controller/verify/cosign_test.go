package verify

import (
	"context"
	"errors"
	"testing"

	portagerv1alpha1 "github.com/jarodr47/portager/api/v1alpha1"
)

type mockCosignVerifier struct {
	err error
}

func (m *mockCosignVerifier) VerifySignature(_ context.Context, _ string, _ *portagerv1alpha1.CosignConfig) error {
	return m.err
}

func TestCosignVerifier_VerifySignature(t *testing.T) {
	tests := []struct {
		name    string
		config  *portagerv1alpha1.CosignConfig
		mockErr error
		wantErr bool
	}{
		{
			name:    "nil config returns success",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "disabled returns success",
			config:  &portagerv1alpha1.CosignConfig{Enabled: false},
			wantErr: false,
		},
		{
			name: "enabled with valid signature succeeds",
			config: &portagerv1alpha1.CosignConfig{
				Enabled:   true,
				PublicKey: "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
			},
			mockErr: nil,
			wantErr: false,
		},
		{
			name: "enabled with no signature returns error",
			config: &portagerv1alpha1.CosignConfig{
				Enabled:   true,
				PublicKey: "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
			},
			mockErr: errors.New("no matching signatures found"),
			wantErr: true,
		},
		{
			name: "keyless mode with issuer succeeds",
			config: &portagerv1alpha1.CosignConfig{
				Enabled:       true,
				KeylessIssuer: "https://accounts.google.com",
			},
			mockErr: nil,
			wantErr: false,
		},
		{
			name: "keyless mode with issuer fails",
			config: &portagerv1alpha1.CosignConfig{
				Enabled:       true,
				KeylessIssuer: "https://accounts.google.com",
			},
			mockErr: errors.New("no matching signatures found"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockCosignVerifier{err: tt.mockErr}
			// Test through the Validator to exercise the nil/disabled short-circuits
			v := &Validator{CosignVerifier: mock}
			var validationConfig *portagerv1alpha1.ValidationConfig
			if tt.config != nil {
				validationConfig = &portagerv1alpha1.ValidationConfig{Cosign: tt.config}
			}
			result := v.Validate(context.Background(), "fake-registry.invalid/test:latest", validationConfig, nil)

			if tt.wantErr && result.Verified {
				t.Errorf("expected verification to fail but it succeeded")
			}
			if !tt.wantErr && !result.Verified {
				t.Errorf("expected verification to succeed but got error: %s", result.Error)
			}
		})
	}
}

func TestCosignVerifier_ConfigValidation(t *testing.T) {
	verifier := NewCosignVerifier()

	t.Run("enabled with neither key nor issuer returns error", func(t *testing.T) {
		config := &portagerv1alpha1.CosignConfig{
			Enabled: true,
		}
		err := verifier.VerifySignature(context.Background(), "fake-registry.invalid/test:latest", config)
		if err == nil {
			t.Error("expected error when neither publicKey nor keylessIssuer is set")
		}
	})
}
