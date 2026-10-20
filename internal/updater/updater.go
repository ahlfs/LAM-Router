// Package updater reports the running version and checks the npm registry for a
// newer release.
//
// LAM-Router never replaces its own binary: npm (or the operator's build step)
// owns the file on disk. This package is deliberately read-only — it fetches
// release metadata and prints the command a human should run.
//
// The pre-2.0.0 write path (download -> chmod 0755 -> overwrite the running
// executable -> restart self) was removed: it verified nothing, kept no rollback
// copy, and fought the process supervisor that owns the service.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"lamrouter/internal/log"
)

// CurrentVersion is the running lam-router version. Release builds inject it:
//
//	-ldflags "-X lamrouter/internal/updater.CurrentVersion=2.0.0"
var CurrentVersion = "0.0.0-dev"

// PackageName is the npm package that carries the release binaries.
const PackageName = "lam-router"

// PackagePage is the npm landing page for PackageName.
const PackagePage = "https://www.npmjs.com/package/" + PackageName

// RegistryURL is the npm registry queried for version checks. Override it with
// LAMROUTER_NPM_REGISTRY (tests, and installs behind a mirror).
var RegistryURL = "https://registry.npmjs.org"

// UpdateCommand installs the newest release; RestartHint makes the running
// service pick up the new binary.
const (
	UpdateCommand = "npm i -g " + PackageName + "@latest"
	RestartHint   = "pm2 restart lamrouter"
)

// UpdateInfo is the JSON contract served to the dashboard and printed by the CLI.
// The 1.x field names are kept. 2.0.0 drops releaseNotes in favour of releaseUrl
// and adds the npm install-channel fields.
type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	DownloadURL    string `json:"downloadUrl"`
	ReleaseURL     string `json:"releaseUrl"`
	GoVersion      string `json:"goVersion"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	CheckedAt      string `json:"checkedAt"`

	// npm install channel (2.0.0+)
	InstallChannel string `json:"installChannel"`
	InstallPath    string `json:"installPath"`
	UpdateCommand  string `json:"updateCommand"`
	RestartHint    string `json:"restartHint"`
}

var (
	cachedInfo *UpdateInfo
	cacheMu    sync.RWMutex
)

// GetCachedInfo returns the last check result, or a local-only snapshot when no
// network check has run yet: the installed version and channel are always known
// offline, only the published version is not.
func GetCachedInfo() *UpdateInfo {
	cacheMu.RLock()
	if cachedInfo != nil {
		info := *cachedInfo
		cacheMu.RUnlock()
		return &info
	}
	cacheMu.RUnlock()

	channel, path := DetectInstallChannel()
	return &UpdateInfo{
		CurrentVersion: CurrentVersion,
		LatestVersion:  CurrentVersion,
		HasUpdate:      false,
		DownloadURL:    PackagePage,
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
		InstallChannel: channel,
		InstallPath:    path,
		UpdateCommand:  UpdateCommand,
		RestartHint:    RestartHint,
	}
}

// CheckUpdate queries the npm registry for the newest published version and
// reports whether this binary is behind it.
func CheckUpdate(ctx context.Context) (*UpdateInfo, error) {
	latest, err := fetchLatestVersion(ctx)
	if err != nil {
		return nil, err
	}

	current := strings.TrimPrefix(CurrentVersion, "v")
	channel, path := DetectInstallChannel()

	info := &UpdateInfo{
		CurrentVersion: CurrentVersion,
		LatestVersion:  latest,
		HasUpdate:      CompareVersions(latest, current) > 0,
		DownloadURL:    PackagePage,
		ReleaseURL:     ReleaseURLFor(latest),
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		CheckedAt:      time.Now().UTC().Format(time.RFC3339),
		InstallChannel: channel,
		InstallPath:    path,
		UpdateCommand:  UpdateCommand,
		RestartHint:    RestartHint,
	}

	cacheMu.Lock()
	cachedInfo = info
	cacheMu.Unlock()

	return info, nil
}

// ReleaseURLFor is the GitHub release page for a version ("" when version is empty).
func ReleaseURLFor(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return ""
	}
	return "https://github.com/ahlfs/LAM-Router/releases/tag/v" + version
}

// npmVersionDoc is the subset of the registry's /<pkg>/latest document we need.
type npmVersionDoc struct {
	Version    string `json:"version"`
	Deprecated string `json:"deprecated"`
}

// fetchLatestVersion reads the "latest" dist-tag document from the npm registry.
func fetchLatestVersion(ctx context.Context) (string, error) {
	base := os.Getenv("LAMROUTER_NPM_REGISTRY")
	if base == "" {
		base = RegistryURL
	}
	url := strings.TrimRight(base, "/") + "/" + PackageName + "/latest"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build npm registry request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query npm registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry returned HTTP %d for %s", resp.StatusCode, PackageName)
	}

	// A version document is a few KB. Cap the read so a broken or hostile
	// registry cannot stream unbounded data into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return "", fmt.Errorf("read npm registry response: %w", err)
	}

	var doc npmVersionDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("parse npm registry response: %w", err)
	}

	latest := strings.TrimPrefix(strings.TrimSpace(doc.Version), "v")
	if latest == "" {
		return "", fmt.Errorf("npm registry response for %s carries no version", PackageName)
	}
	if doc.Deprecated != "" {
		log.Info("updater", "published release is marked deprecated on npm",
			"version", latest, "reason", doc.Deprecated)
	}
	return latest, nil
}

// StartBackgroundCheck logs a notice when a newer release exists. It installs
// nothing: updating is an explicit user action (`lamrouter update`).
func StartBackgroundCheck(enabled bool) {
	if !enabled {
		return
	}
	go func() {
		time.Sleep(3 * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		info, err := CheckUpdate(ctx)
		if err != nil {
			log.Debug("updater", "update check skipped/failed", "error", err)
			return
		}
		if info.HasUpdate {
			log.Info("updater", "NEW RELEASE AVAILABLE on npm",
				"current", info.CurrentVersion, "latest", info.LatestVersion,
				"update", info.UpdateCommand, "restart", info.RestartHint)
			return
		}
		log.Debug("updater", "app is up to date", "version", info.CurrentVersion)
	}()
}

// CompareVersions returns 1 when v1 is newer than v2, -1 when older, 0 when equal.
func CompareVersions(v1, v2 string) int {
	parts1 := parseSemver(v1)
	parts2 := parseSemver(v2)

	for i := 0; i < 3; i++ {
		if parts1[i] > parts2[i] {
			return 1
		}
		if parts1[i] < parts2[i] {
			return -1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	var parts [3]int
	fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
	return parts
}
