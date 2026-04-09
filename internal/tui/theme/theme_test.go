package theme_test

import (
	"testing"

	"github.com/Mizune/mosaic/internal/tui/theme"
)

func TestByName_DefaultsToMocha(t *testing.T) {
	cases := []string{"", "unknown", "catppuccin", "mosaic"}
	for _, name := range cases {
		got := theme.ByName(name)
		if got.Name != "catppuccin-mocha" {
			t.Errorf("ByName(%q).Name = %q, want catppuccin-mocha", name, got.Name)
		}
	}
}

func TestByName_ResolvesEveryBuiltIn(t *testing.T) {
	cases := map[string]string{
		"catppuccin-mocha":  "catppuccin-mocha",
		"catppuccin-latte":  "catppuccin-latte",
		"tokyo-night":       "tokyo-night",
		"tokyo-night-storm": "tokyo-night-storm",
		"system":            "system",
	}
	for name, want := range cases {
		if got := theme.ByName(name); got.Name != want {
			t.Errorf("ByName(%q) = %q, want %q", name, got.Name, want)
		}
	}
}

func TestByName_CaseAndSeparatorInsensitive(t *testing.T) {
	got := theme.ByName("Tokyo_Night")
	if got.Name != "tokyo-night" {
		t.Errorf("got %q, want tokyo-night", got.Name)
	}
}

func TestAvailable_ListsEveryTheme(t *testing.T) {
	if got := theme.Available(); len(got) != 5 {
		t.Errorf("Available() returned %d themes, want 5", len(got))
	}
}
