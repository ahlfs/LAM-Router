package webdist

import (
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// The dashboard's TypeScript theme definitions are not in this repository, so
// the light themes' design tokens can only be guarded from the built
// stylesheet. Every light theme shipped --muted set to the same dark value as
// --muted-foreground, while every var(--muted) use in the stylesheet is a
// background-color with 10-80% alpha. On white cards that turned the panels
// using it (Universal Base URL, Cloudflare Tunnel, the keys table header and
// its filter bar) into grey slabs that read as disabled. A light theme must
// keep --muted a light surface, ideally tinted from its own palette.
func TestEmbeddedLightThemesUseALightMutedSurface(t *testing.T) {
	css := readEmbeddedAsset(t, "assets/index-*.css")

	themes := lightThemeBlocks(css)
	if len(themes) < 17 {
		t.Fatalf("expected the 17 *-light theme blocks, found %d", len(themes))
	}

	for _, name := range sortedKeys(themes) {
		body := themes[name]
		muted := cssDeclaration(body, "--muted")
		mutedFG := cssDeclaration(body, "--muted-foreground")

		if muted == "" {
			t.Errorf("%s: no --muted declaration", name)
			continue
		}
		if muted == mutedFG && mutedFG != "" {
			t.Errorf("%s: --muted is %q, the --muted-foreground value; a light theme needs a surface here", name, muted)
			continue
		}

		lum, ok := cssRelativeLuminance(muted)
		if !ok {
			t.Errorf("%s: cannot parse --muted %q", name, muted)
			continue
		}
		if lum < 0.40 {
			t.Errorf("%s: --muted %s is a dark surface in a light theme (relative luminance %.3f < 0.40)", name, muted, lum)
		}
	}

	// The base light theme (plain "light" in the picker) is the :root block,
	// which cannot be found by the data-theme selector.
	base := cssBlockContaining(css, "--secondary:oklch(96% 0 0)")
	if base == "" {
		t.Fatal("base light theme block not found")
	}
	baseMuted := cssDeclaration(base, "--muted")
	if lum, ok := cssRelativeLuminance(baseMuted); !ok || lum < 0.40 {
		t.Errorf("base light theme: --muted %s is a dark surface (luminance %.3f)", baseMuted, lum)
	}
}

// lightThemeBlocks maps each [data-theme="*-light"] selector to its declaration
// body.
func lightThemeBlocks(css string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`\[data-theme="?([A-Za-z0-9_-]+)"?\]\s*\{`)
	for _, m := range re.FindAllStringSubmatchIndex(css, -1) {
		name := css[m[2]:m[3]]
		if !strings.HasSuffix(name, "-light") {
			continue
		}
		if body, ok := cssBalancedBlock(css, m[1]-1); ok {
			out[name] = body
		}
	}
	return out
}

// cssBlockContaining returns the declaration body of the innermost block that
// contains needle, or "" when no balanced block does.
func cssBlockContaining(css, needle string) string {
	idx := strings.Index(css, needle)
	if idx < 0 {
		return ""
	}
	open := strings.LastIndex(css[:idx], "{")
	if open < 0 {
		return ""
	}
	body, ok := cssBalancedBlock(css, open)
	if !ok {
		return ""
	}
	return body
}

// cssBalancedBlock returns the text between the brace at openIdx and its match.
func cssBalancedBlock(css string, openIdx int) (string, bool) {
	depth := 0
	for i := openIdx; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[openIdx+1 : i], true
			}
		}
	}
	return "", false
}

// cssDeclaration reads a custom property out of a declaration body.
func cssDeclaration(body, name string) string {
	re := regexp.MustCompile(`(?:^|;)\s*` + regexp.QuoteMeta(name) + `:\s*([^;]+)`)
	if m := re.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// cssRelativeLuminance converts the colour forms this stylesheet uses into a
// WCAG relative luminance, so one threshold covers hex and oklch alike. An
// oklch lightness is perceptual, so it goes through the CIE L* formula (chroma
// is ignored, which is enough for a surface that must simply stay light).
func cssRelativeLuminance(value string) (float64, bool) {
	v := strings.TrimSpace(value)

	switch {
	case strings.HasPrefix(v, "#"):
		h := strings.TrimPrefix(v, "#")
		if len(h) == 3 || len(h) == 4 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if len(h) != 6 && len(h) != 8 {
			return 0, false
		}
		var rgb [3]float64
		for i := 0; i < 3; i++ {
			n, err := strconv.ParseUint(h[i*2:i*2+2], 16, 8)
			if err != nil {
				return 0, false
			}
			rgb[i] = float64(n) / 255
		}
		return relativeLuminance(rgb), true

	case strings.HasPrefix(v, "oklch("):
		inner := strings.TrimSuffix(strings.TrimPrefix(v, "oklch("), ")")
		fields := strings.Fields(strings.ReplaceAll(inner, "/", " "))
		if len(fields) == 0 {
			return 0, false
		}
		pct := strings.HasSuffix(fields[0], "%")
		l, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], "%"), 64)
		if err != nil {
			return 0, false
		}
		if !pct {
			l *= 100
		}
		return luminanceFromLightnessPercent(l), true
	}
	return 0, false
}

func luminanceFromLightnessPercent(l float64) float64 {
	if l <= 0 {
		return 0
	}
	if l <= 8 {
		return l / 903.3
	}
	t := (l + 16) / 116
	return t * t * t
}

func relativeLuminance(rgb [3]float64) float64 {
	lin := func(c float64) float64 {
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(rgb[0]) + 0.7152*lin(rgb[1]) + 0.0722*lin(rgb[2])
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// A guard that cannot fail is decoration: feed it the exact values 2.0.1
// shipped and require it to complain.
func TestLightMutedGuardRejectsTheOldValues(t *testing.T) {
	for _, c := range []struct {
		name  string
		value string
	}{
		{"claude-nous-light", "#546b85"},
		{"matrix-light", "#062a128c"},
		{"base light theme", "oklch(55% 0 0)"},
	} {
		lum, ok := cssRelativeLuminance(c.value)
		if !ok {
			t.Fatalf("%s: value %q must be parseable", c.name, c.value)
		}
		if lum >= 0.40 {
			t.Errorf("%s: %s scores %.3f, which the guard would accept; the old grey must be rejected", c.name, c.value, lum)
		}
	}

	// And the shipped values must pass, so the threshold is not simply strict.
	for _, c := range []struct {
		name  string
		value string
	}{
		{"dark-minimalist-light", "#ced0d3"},
		{"base light theme", "#cfcfcf"},
		{"biolab-light", "#b6dbc1"},
	} {
		lum, ok := cssRelativeLuminance(c.value)
		if !ok || lum < 0.40 {
			t.Errorf("%s: %s scores %.3f, expected a light surface", c.name, c.value, lum)
		}
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
