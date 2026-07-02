package profile

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeStore struct {
	value Profile
	err   error
}

func (f *fakeStore) Upsert(context.Context, Profile) error {
	return f.err
}

func (f *fakeStore) Get(context.Context, string) (Profile, error) {
	return f.value, f.err
}

func TestManagerLoadsProfile(t *testing.T) {
	manager, err := NewManager(&fakeStore{value: Profile{
		UserID:         "oncall-1",
		DefaultService: "checkout",
	}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	value, err := manager.LoadProfile(context.Background(), " oncall-1 ")
	if err != nil || value.DefaultService != "checkout" {
		t.Fatalf("LoadProfile() value=%#v error=%v", value, err)
	}
}

func TestManagerPreservesMissingProfile(t *testing.T) {
	manager, _ := NewManager(&fakeStore{err: ErrNotFound})
	_, err := manager.LoadProfile(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadProfile() error = %v, want ErrNotFound", err)
	}
}

func TestContextLinesAreBoundedAndExcludeMetadata(t *testing.T) {
	value := Profile{
		DefaultService: "checkout",
		Services: []string{
			"checkout", "payment", "redis", "catalog", "orders",
			"shipping", "inventory", "gateway", "auth", "search", "ignored",
		},
		Timezone: "Australia/Melbourne",
		Preferences: map[string]any{
			"notification_style": "concise",
			"include_runbooks":   true,
			"nested":             map[string]any{"secret": "excluded"},
		},
		Metadata: map[string]any{"private_note": "must not be rendered"},
	}

	lines := ContextLines(value)
	combined := strings.Join(lines, "\n")
	if len(lines) != 4 ||
		!strings.Contains(combined, "default_service=checkout") ||
		!strings.Contains(combined, "timezone=Australia/Melbourne") ||
		!strings.Contains(combined, "notification_style:concise") ||
		strings.Contains(combined, "private_note") ||
		strings.Contains(combined, "secret") ||
		strings.Contains(combined, "ignored") {
		t.Fatalf("ContextLines() = %#v", lines)
	}
	for _, line := range lines {
		if len([]rune(line)) > maxContextLineSize {
			t.Fatalf("context line too long: %d", len([]rune(line)))
		}
	}
}
