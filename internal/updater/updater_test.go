package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.1", "1.0.0", 1},
		{"v1.2.3", "1.2.3", 0},
		{"v1.2.4", "v1.2.3", 1},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.v1, tt.v2)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestGetCachedInfo(t *testing.T) {
	info := GetCachedInfo()
	if info == nil {
		t.Fatal("expected non-nil UpdateInfo")
	}
	if info.CurrentVersion == "" {
		t.Error("expected non-empty CurrentVersion")
	}
	if info.UpdateCommand != UpdateCommand {
		t.Errorf("UpdateCommand = %q, want %q", info.UpdateCommand, UpdateCommand)
	}
	if info.InstallChannel == "" {
		t.Error("expected a resolved install channel even before any network check")
	}
}

// stubRegistry serves a /lam-router/latest document from our own test server so
// version checks are exercised without touching the network.
func stubRegistry(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+PackageName+"/latest" {
			t.Errorf("registry called with unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckUpdateReadsNPMRegistry(t *testing.T) {
	srv := stubRegistry(t, `{"name":"lam-router","version":"99.0.0"}`, http.StatusOK)
	t.Setenv("LAMROUTER_NPM_REGISTRY", srv.URL)

	info, err := CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if info.LatestVersion != "99.0.0" {
		t.Errorf("LatestVersion = %q, want 99.0.0", info.LatestVersion)
	}
	if !info.HasUpdate {
		t.Error("expected HasUpdate=true for a newer published version")
	}
	if info.UpdateCommand == "" {
		t.Error("expected a non-empty UpdateCommand")
	}
	if info.ReleaseURL == "" {
		t.Error("expected a release URL for the newer version")
	}
}

func TestCheckUpdateCachesResult(t *testing.T) {
	srv := stubRegistry(t, `{"name":"lam-router","version":"99.0.1"}`, http.StatusOK)
	t.Setenv("LAMROUTER_NPM_REGISTRY", srv.URL)

	if _, err := CheckUpdate(context.Background()); err != nil {
		t.Fatalf("CheckUpdate: %v", err)
	}
	if got := GetCachedInfo().LatestVersion; got != "99.0.1" {
		t.Errorf("GetCachedInfo().LatestVersion = %q, want 99.0.1", got)
	}
}

func TestCheckUpdateReportsRegistryFailure(t *testing.T) {
	srv := stubRegistry(t, `{"error":"boom"}`, http.StatusInternalServerError)
	t.Setenv("LAMROUTER_NPM_REGISTRY", srv.URL)

	if _, err := CheckUpdate(context.Background()); err == nil {
		t.Fatal("expected an error when the registry responds 500")
	}
}

func TestCheckUpdateRejectsDocumentWithoutVersion(t *testing.T) {
	srv := stubRegistry(t, `{"name":"lam-router"}`, http.StatusOK)
	t.Setenv("LAMROUTER_NPM_REGISTRY", srv.URL)

	if _, err := CheckUpdate(context.Background()); err == nil {
		t.Fatal("expected an error when the registry document carries no version")
	}
}

func TestCheckUpdateReportsOfflineRegistry(t *testing.T) {
	// A closed port: no listener, so the request fails and the caller sees it.
	t.Setenv("LAMROUTER_NPM_REGISTRY", "http://127.0.0.1:1")

	if _, err := CheckUpdate(context.Background()); err == nil {
		t.Fatal("expected an error when the registry is unreachable")
	}
}

func TestDetectInstallChannelIsManualForTestBinary(t *testing.T) {
	channel, path := DetectInstallChannel()
	if channel != ChannelManual {
		t.Errorf("channel = %q, want %q for a go test binary", channel, ChannelManual)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("path = %q, want an absolute path", path)
	}
	if IsNPMInstall() {
		t.Error("IsNPMInstall() = true for a go test binary")
	}
}

func TestNPMUpdateCommandPinsVersion(t *testing.T) {
	if got, want := NPMUpdateCommand("2.0.0"), "npm i -g lam-router@2.0.0"; got != want {
		t.Errorf("NPMUpdateCommand(2.0.0) = %q, want %q", got, want)
	}
	if got, want := NPMUpdateCommand("v2.0.0"), "npm i -g lam-router@2.0.0"; got != want {
		t.Errorf("NPMUpdateCommand(v2.0.0) = %q, want %q", got, want)
	}
	if got := NPMUpdateCommand(""); got != UpdateCommand {
		t.Errorf("NPMUpdateCommand(\"\") = %q, want %q", got, UpdateCommand)
	}
}

// TestUpdateIsDelegatedToNPM pins the 2.0.0 decision: LAM-Router never installs
// into itself, so the package must not expose a self-replacing helper. If a
// write path is ever reintroduced it must come with checksum verification, a
// retained backup and supervisor-aware restart — and this test deleted on
// purpose.
func TestUpdateIsDelegatedToNPM(t *testing.T) {
	if got := NPMUpdateCommand("2.0.0"); got[:10] != "npm i -g l" {
		t.Errorf("update target is not npm: %q", got)
	}
	if PackageName != "lam-router" {
		t.Errorf("PackageName = %q, want lam-router", PackageName)
	}
}
