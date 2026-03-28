package tags

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
)

// TagLister lists all tags for a repository from a registry.
type TagLister interface {
	ListTags(ctx context.Context, repository string, auth authn.Authenticator) ([]string, error)
}

// CraneTagLister implements TagLister using crane.ListTags.
type CraneTagLister struct{}

// ListTags lists all tags for a repository using the Docker Registry V2 API.
func (c *CraneTagLister) ListTags(ctx context.Context, repository string, auth authn.Authenticator) ([]string, error) {
	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuth(auth),
	}
	if isInsecureRegistry(repository) {
		opts = append(opts, crane.Insecure)
	}

	tags, err := crane.ListTags(repository, opts...)
	if err != nil {
		return nil, fmt.Errorf("listing tags for %s: %w", repository, err)
	}
	return tags, nil
}

// tagVersion pairs the original tag string with its parsed semver version.
type tagVersion struct {
	original string
	version  *semver.Version
}

// SemverResolver resolves semver constraints against registry tags.
type SemverResolver struct {
	Lister TagLister
}

// ResolveTags lists tags from the registry, filters by the semver constraint,
// sorts descending (newest first), and truncates to maxTags.
// Returns the original tag strings (not coerced versions).
func (r *SemverResolver) ResolveTags(ctx context.Context, repository string, constraint string, maxTags int, auth authn.Authenticator) ([]string, error) {
	// Parse the constraint first to fail fast on invalid input.
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid semver constraint %q: %w", constraint, err)
	}

	// List all tags from the registry.
	allTags, err := r.Lister.ListTags(ctx, repository, auth)
	if err != nil {
		return nil, err
	}

	// Parse each tag as semver, filter by constraint.
	var matched []tagVersion
	for _, tag := range allTags {
		v, err := semver.NewVersion(tag)
		if err != nil {
			// Not a valid semver tag (e.g., "latest", "alpine") — skip silently.
			continue
		}
		if c.Check(v) {
			matched = append(matched, tagVersion{original: tag, version: v})
		}
	}

	// Sort by semver descending (newest first).
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].version.GreaterThan(matched[j].version)
	})

	// Truncate to maxTags if set.
	if maxTags > 0 && len(matched) > maxTags {
		matched = matched[:maxTags]
	}

	// Extract original tag strings.
	result := make([]string, len(matched))
	for i, tv := range matched {
		result[i] = tv.original
	}
	return result, nil
}

// isInsecureRegistry returns true for registries that use HTTP (not HTTPS).
func isInsecureRegistry(ref string) bool {
	return strings.HasPrefix(ref, "localhost") ||
		strings.HasPrefix(ref, "localhost:") ||
		strings.HasPrefix(ref, "127.0.0.1")
}
