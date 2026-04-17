package config_test

import (
	"testing"

	"github.com/Mizune/mosaic/internal/config"
)

func TestBoolPtr(t *testing.T) {
	if got := config.BoolPtr(true); got == nil || *got != true {
		t.Errorf("BoolPtr(true) = %v, want pointer to true", got)
	}
	if got := config.BoolPtr(false); got == nil || *got != false {
		t.Errorf("BoolPtr(false) = %v, want pointer to false", got)
	}
}

func TestBoolOr(t *testing.T) {
	cases := []struct {
		name     string
		p        *bool
		fallback bool
		want     bool
	}{
		{"nil pointer uses fallback true", nil, true, true},
		{"nil pointer uses fallback false", nil, false, false},
		{"non-nil true wins over fallback false", config.BoolPtr(true), false, true},
		{"non-nil false wins over fallback true", config.BoolPtr(false), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := config.BoolOr(tc.p, tc.fallback); got != tc.want {
				t.Errorf("BoolOr(%v, %v) = %v, want %v", tc.p, tc.fallback, got, tc.want)
			}
		})
	}
}
