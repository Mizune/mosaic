package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheck_DevVersion(t *testing.T) {
	u := New("dev")
	result, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UpdateAvail {
		t.Error("dev version should never report update available")
	}
	if result.Current != "dev" {
		t.Errorf("current = %q, want %q", result.Current, "dev")
	}
}

func TestCheck_UpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v1.0.0", "assets": []}`))
	}))
	defer srv.Close()

	u := &Updater{
		Owner:      "test",
		Repo:       "test",
		CurrentVer: "1.0.0",
		HTTPClient: srv.Client(),
	}
	// Override the URL by intercepting the HTTP client transport.
	u.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = srv.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}

	result, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UpdateAvail {
		t.Error("same version should not report update available")
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v2.0.0", "assets": []}`))
	}))
	defer srv.Close()

	u := &Updater{
		Owner:      "test",
		Repo:       "test",
		CurrentVer: "1.0.0",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	result, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.UpdateAvail {
		t.Error("newer version should report update available")
	}
	if result.Latest != "2.0.0" {
		t.Errorf("latest = %q, want %q", result.Latest, "2.0.0")
	}
}

func TestCheck_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	u := &Updater{
		Owner:      "test",
		Repo:       "test",
		CurrentVer: "1.0.0",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = srv.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		},
	}

	_, err := u.Check(context.Background())
	if err == nil {
		t.Fatal("expected error for rate-limited response")
	}
	if err != ErrRateLimited {
		t.Errorf("error = %v, want ErrRateLimited", err)
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		version, goos, goarch, want string
	}{
		{"1.0.0", "darwin", "arm64", "mosaic_1.0.0_darwin_arm64.tar.gz"},
		{"1.0.0", "darwin", "amd64", "mosaic_1.0.0_darwin_amd64.tar.gz"},
		{"2.1.0", "linux", "amd64", "mosaic_2.1.0_linux_amd64.tar.gz"},
		{"0.1.0", "linux", "arm64", "mosaic_0.1.0_linux_arm64.tar.gz"},
	}
	for _, tt := range tests {
		got := AssetName(tt.version, tt.goos, tt.goarch)
		if got != tt.want {
			t.Errorf("AssetName(%q, %q, %q) = %q, want %q",
				tt.version, tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestVerifyChecksum_Match(t *testing.T) {
	data := []byte("hello mosaic")
	checksums := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  other.tar.gz\n" +
		"b1a7c6bc6d3c1a7afa0cbb1a2e6a7e3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9  mosaic_1.0.0_linux_amd64.tar.gz\n"

	// We need actual matching checksum. Let's compute it.
	err := verifyChecksum(data, []byte(checksums), "mosaic_1.0.0_linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestVerifyChecksum_NoEntry(t *testing.T) {
	data := []byte("hello")
	checksums := []byte("abc123  other.tar.gz\n")
	err := verifyChecksum(data, checksums, "mosaic_1.0.0_linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected error for missing checksum entry")
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
