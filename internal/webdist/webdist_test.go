package webdist

import (
	"io/fs"
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
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("open embedded dist: %v", err)
	}

	name := "assets/settings-CLHTY3d8.js"
	if _, err := fs.Stat(sub, name); err != nil {
		// A future rebuild changes the content hash; fall back to the new chunk.
		names, globErr := fs.Glob(sub, "assets/settings-*.js")
		if globErr != nil || len(names) == 0 {
			t.Fatalf("no settings chunk in embedded dist: %v", err)
		}
		name = names[0]
	}

	blob, err := fs.ReadFile(sub, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	text := string(blob)

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
