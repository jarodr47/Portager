package auth

import "testing"

func TestGarHost(t *testing.T) {
	tests := []struct {
		name     string
		registry string
		want     string
	}{
		{
			name:     "hostname only",
			registry: "us-central1-docker.pkg.dev",
			want:     "us-central1-docker.pkg.dev",
		},
		{
			name:     "hostname with project",
			registry: "us-central1-docker.pkg.dev/my-project",
			want:     "us-central1-docker.pkg.dev",
		},
		{
			name:     "hostname with project and repo",
			registry: "us-central1-docker.pkg.dev/my-project/my-repo",
			want:     "us-central1-docker.pkg.dev",
		},
		{
			name:     "europe region",
			registry: "europe-west1-docker.pkg.dev/my-project",
			want:     "europe-west1-docker.pkg.dev",
		},
		{
			name:     "asia region hostname only",
			registry: "asia-east1-docker.pkg.dev",
			want:     "asia-east1-docker.pkg.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := garHost(tt.registry)
			if got != tt.want {
				t.Errorf("garHost(%q) = %q, want %q", tt.registry, got, tt.want)
			}
		})
	}
}
