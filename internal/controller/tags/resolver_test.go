package tags

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTags(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tags Suite")
}

// mockTagLister returns a fixed set of tags or an error.
type mockTagLister struct {
	tags []string
	err  error
}

func (m *mockTagLister) ListTags(_ context.Context, _ string, _ authn.Authenticator) ([]string, error) {
	return m.tags, m.err
}

var _ = Describe("SemverResolver", func() {
	var (
		ctx      context.Context
		resolver *SemverResolver
		lister   *mockTagLister
	)

	BeforeEach(func() {
		ctx = context.Background()
		lister = &mockTagLister{}
		resolver = &SemverResolver{Lister: lister}
	})

	Describe("ResolveTags", func() {
		Context("wildcard constraints", func() {
			It("should match 1.x against matching tags", func() {
				lister.tags = []string{"1.0", "1.1", "1.2", "2.0", "latest"}
				tags, err := resolver.ResolveTags(ctx, "docker.io/library/alpine", "1.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.2", "1.1", "1.0"}))
			})

			It("should match 1.3.x against patch versions", func() {
				lister.tags = []string{"1.2.9", "1.3.0", "1.3.1", "1.3.2", "1.4.0"}
				tags, err := resolver.ResolveTags(ctx, "docker.io/library/node", "1.3.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.3.2", "1.3.1", "1.3.0"}))
			})
		})

		Context("range constraints", func() {
			It("should match >= < range", func() {
				lister.tags = []string{"1.21.5", "1.22.0", "1.22.1", "1.23.0"}
				tags, err := resolver.ResolveTags(ctx, "docker.io/library/go", ">= 1.22.0, < 1.23.0", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.22.1", "1.22.0"}))
			})

			It("should match tilde range ~1.3.0", func() {
				lister.tags = []string{"1.2.9", "1.3.0", "1.3.5", "1.4.0"}
				tags, err := resolver.ResolveTags(ctx, "docker.io/library/node", "~1.3.0", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.3.5", "1.3.0"}))
			})

			It("should match caret range ^1.3.0", func() {
				lister.tags = []string{"1.2.9", "1.3.0", "1.5.0", "1.99.0", "2.0.0"}
				tags, err := resolver.ResolveTags(ctx, "docker.io/library/node", "^1.3.0", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.99.0", "1.5.0", "1.3.0"}))
			})
		})

		Context("maxTags truncation", func() {
			It("should truncate to maxTags newest versions", func() {
				lister.tags = []string{"1.0", "1.1", "1.2", "1.3", "1.4"}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 2, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.4", "1.3"}))
			})

			It("should return all when maxTags is 0 (unlimited)", func() {
				lister.tags = []string{"1.0", "1.1", "1.2", "1.3", "1.4"}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 0, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(HaveLen(5))
			})
		})

		Context("non-semver tags", func() {
			It("should silently skip non-semver tags", func() {
				lister.tags = []string{"latest", "alpine", "1.0", "bullseye", "1.1"}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.1", "1.0"}))
			})

			It("should skip tags with non-semver suffixes", func() {
				lister.tags = []string{"1.22-alpine", "1.22.0", "1.22.1-rc.1", "1.22.1"}
				tags, err := resolver.ResolveTags(ctx, "repo", "~1.22.0", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				// Pre-release versions like 1.22.1-rc.1 are valid semver but won't match
				// constraints by default in Masterminds/semver (pre-releases only match
				// if the constraint itself includes a pre-release).
				// 1.22-alpine is not valid semver (skipped).
				Expect(tags).To(Equal([]string{"1.22.1", "1.22.0"}))
			})
		})

		Context("error cases", func() {
			It("should return error for invalid constraint", func() {
				lister.tags = []string{"1.0"}
				_, err := resolver.ResolveTags(ctx, "repo", "not a valid constraint!!!", 10, authn.Anonymous)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid semver constraint"))
			})

			It("should propagate ListTags error", func() {
				lister.err = fmt.Errorf("registry unavailable")
				_, err := resolver.ResolveTags(ctx, "repo", "1.x", 10, authn.Anonymous)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("registry unavailable"))
			})
		})

		Context("edge cases", func() {
			It("should return empty slice when no tags match", func() {
				lister.tags = []string{"2.0", "3.0", "latest"}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(BeEmpty())
			})

			It("should return empty slice when registry has no tags", func() {
				lister.tags = []string{}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(BeEmpty())
			})

			It("should preserve original tag strings (not coerced versions)", func() {
				// "1.22" is coerced to "1.22.0" for matching, but returned as "1.22"
				lister.tags = []string{"1.22", "1.23"}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"1.23", "1.22"}))
			})

			It("should handle v-prefixed tags", func() {
				lister.tags = []string{"v1.0.0", "v1.1.0", "v2.0.0"}
				tags, err := resolver.ResolveTags(ctx, "repo", "1.x", 10, authn.Anonymous)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal([]string{"v1.1.0", "v1.0.0"}))
			})
		})
	})
})
