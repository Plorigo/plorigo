package deployments

import (
	"regexp"
	"strings"
	"testing"
)

var dnsLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func TestPreviewRouteHost_PrettyAndCollisionSafe(t *testing.T) {
	h1 := previewRouteHost("my-app", 7, "feat", "service-aaaa")
	if !strings.HasPrefix(h1, "my-app-pr-7-") {
		t.Errorf("host = %q, want my-app-pr-7-<hash>", h1)
	}
	if !dnsLabelRe.MatchString(h1) || len(h1) > maxRouteKeyLen {
		t.Errorf("host %q is not a valid DNS label (<= %d chars)", h1, maxRouteKeyLen)
	}

	// Same slug + PR but a different service id → a different host (collision-safe).
	h2 := previewRouteHost("my-app", 7, "feat", "service-bbbb")
	if h1 == h2 {
		t.Errorf("two services sharing a slug must not collide on the same host: %q", h1)
	}

	// A branch preview uses the branch slug.
	hb := previewRouteHost("my-app", 0, "feature/x", "service-aaaa")
	if !strings.HasPrefix(hb, "my-app-feature-x-") {
		t.Errorf("branch host = %q, want my-app-feature-x-<hash>", hb)
	}
}

func TestPreviewRouteHost_LongSlugStaysWithinLabel(t *testing.T) {
	h := previewRouteHost(strings.Repeat("a", 80), 7, "feat", "svc")
	if len(h) > maxRouteKeyLen {
		t.Errorf("host is %d chars, want <= %d: %q", len(h), maxRouteKeyLen, h)
	}
	if !dnsLabelRe.MatchString(h) {
		t.Errorf("truncated host %q is not a valid DNS label", h)
	}
}

func TestPreviewRouteHost_EmptySlugDefaults(t *testing.T) {
	h := previewRouteHost("", 7, "feat", "svc")
	if !strings.HasPrefix(h, "preview-pr-7-") {
		t.Errorf("host = %q, want a preview- default when the slug is empty", h)
	}
}
