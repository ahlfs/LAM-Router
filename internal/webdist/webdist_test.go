package webdist

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dashboard's System Diagnostics panel ships as a prebuilt bundle whose
// sources live outside this repository, so a regression inside it is invisible
// to the Go compiler. These assertions catch the 1.8.x class of bug: a bundle
// that still advertises an update recipe which was never right for a Go binary.
func TestEmbeddedBundleHasNoStaleUpdateRecipe(t *testing.T) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("open embedded dist: %v", err)
	}

	assets, err := fs.Glob(sub, "assets/*.js")
	if err != nil {
		t.Fatalf("glob embedded assets: %v", err)
	}
	if len(assets) == 0 {
		t.Fatal("no embedded JS assets found")
	}

	// A Go binary is never upgraded with a frontend recipe, and 2.0.0 replaced
	// this hint with the npm command the CLI actually understands.
	banned := []string{"git pull origin main", "pnpm install"}
	for _, name := range assets {
		blob, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, b := range banned {
			if strings.Contains(string(blob), b) {
				t.Errorf("embedded asset %s still contains %q", name, b)
			}
		}
	}
}

func TestEmbeddedBundleOffersNPMUpdate(t *testing.T) {
	text := readEmbeddedAsset(t, "assets/settings-*.js")

	if !strings.Contains(text, "npm i -g lam-router@latest") {
		t.Error("settings bundle does not offer the npm update command")
	}
	if !strings.Contains(text, "/version") {
		t.Error("settings bundle does not read the running version from the backend /version endpoint")
	}
	if strings.Contains(text, `="0.1.3"`) {
		t.Error("settings bundle still pins the stale SPA version constant 0.1.3")
	}
}

// The update hook hands its fields straight to JSX as text, so a payload that
// is forwarded unconsumed makes React reject the render (error #31, "Objects are
// not valid as a React child") and the whole route collapses into its error
// boundary. Verified on a live instance: /version answers {currentVersion,
// latestVersion, hasUpdate, ...}, which is why every field must be unwrapped.
func TestEmbeddedBundleVersionHookConsumesVersionPayload(t *testing.T) {
	text := readEmbeddedAsset(t, "assets/settings-*.js")

	banned := []string{
		"latestVersion:s??null",
		"releases/tag/${s??re}",
	}
	for _, b := range banned {
		if strings.Contains(text, b) {
			t.Errorf("settings bundle forwards the raw /version payload to JSX (%q)", b)
		}
	}

	required := []string{
		"latestVersion:s&&s.latestVersion?s.latestVersion:null",
		"s&&s.releaseUrl?s.releaseUrl",
	}
	for _, r := range required {
		if !strings.Contains(text, r) {
			t.Errorf("settings bundle does not consume /version field-wise (missing %q)", r)
		}
	}
}

// The bundle is hand-maintained (its sources live outside this repository), so
// its static version labels drift silently: 2.0.0 shipped a login screen that
// still announced "v1.8.4 Live". Tie them to the npm package version, which the
// release process bumps before tagging.
func TestEmbeddedBundleVersionLabelsMatchPackage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "npm", "package.json"))
	if err != nil {
		t.Fatalf("read npm/package.json: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("parse npm/package.json: %v", err)
	}
	if pkg.Version == "" {
		t.Fatal("npm/package.json has an empty version")
	}

	settings := readEmbeddedAsset(t, "assets/settings-*.js")
	if want := `Ce="` + pkg.Version + `"`; !strings.Contains(settings, want) {
		t.Errorf("settings bundle version constant is not %q", want)
	}

	index := readEmbeddedAsset(t, "assets/index-*.js")
	if want := `"v` + pkg.Version + ` Live"`; !strings.Contains(index, want) {
		t.Errorf("login screen version label is not %q", want)
	}
}

// readEmbeddedAsset concatenates every embedded asset matching the glob, so a
// rebuild that renames the content-hashed chunk keeps the assertions working.
func readEmbeddedAsset(t *testing.T, pattern string) string {
	t.Helper()

	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("open embedded dist: %v", err)
	}
	names, err := fs.Glob(sub, pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(names) == 0 {
		t.Fatalf("no embedded asset matches %s", pattern)
	}

	var sb strings.Builder
	for _, name := range names {
		blob, err := fs.ReadFile(sub, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sb.Write(blob)
	}
	return sb.String()
}
