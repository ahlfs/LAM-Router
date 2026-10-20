package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Install channels reported by DetectInstallChannel.
const (
	// ChannelNPM means npm owns the binary: it lives inside a node_modules tree
	// (Pattern A platform package placed it there), so npm must update it.
	ChannelNPM = "npm"
	// ChannelManual means a human put the binary there: make install, a Docker
	// image, or a plain copy into /usr/local/bin.
	ChannelManual = "manual"
)

// DetectInstallChannel reports how the running binary was installed and the
// resolved path of that binary.
func DetectInstallChannel() (channel, path string) {
	exe, err := os.Executable()
	if err != nil {
		return ChannelManual, ""
	}
	// A global npm install exposes a symlink (bin/lam-router -> ../lib/... or a
	// node shim), so resolve it before deciding who owns the file.
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}

	slash := filepath.ToSlash(exe)
	switch {
	case strings.Contains(slash, "/node_modules/lam-router-"),
		strings.Contains(slash, "/node_modules/lam-router/"),
		strings.Contains(slash, "/node_modules/.bin/"):
		return ChannelNPM, exe
	default:
		return ChannelManual, exe
	}
}

// IsNPMInstall reports whether npm owns the running binary.
func IsNPMInstall() bool {
	channel, _ := DetectInstallChannel()
	return channel == ChannelNPM
}

// NPMUpdateCommand is the npm command that installs a specific release, or the
// newest one when version is empty.
func NPMUpdateCommand(version string) string {
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return UpdateCommand
	}
	return fmt.Sprintf("npm i -g %s@%s", PackageName, target)
}

// RunNPMUpdate delegates the update to npm and streams its output to the user.
// It never edits files inside node_modules itself — npm owns that directory, and
// npm is also what keeps the platform package layout consistent.
func RunNPMUpdate(version string) error {
	npmBin, err := exec.LookPath("npm")
	if err != nil {
		return fmt.Errorf("npm not found in PATH; install Node.js >= 18, or update with: %s",
			NPMUpdateCommand(version))
	}

	cmd := exec.Command(npmBin, "install", "-g", PackageName+"@"+npmTarget(version))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install -g failed (%v); if the global prefix is root-owned, run: sudo %s",
			err, NPMUpdateCommand(version))
	}
	return nil
}

func npmTarget(version string) string {
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return "latest"
	}
	return target
}
