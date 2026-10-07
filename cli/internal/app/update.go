package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/lipgloss"
	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/urfave/cli/v2"
)

const (
	githubReleasesAPI = "https://api.github.com/repos/speakeasy-api/gram/releases"
	githubDownloadURL = "https://github.com/speakeasy-api/gram/releases/download"

	// homebrewFormula is the Homebrew formula that installs the speakeasy
	// command.
	homebrewFormula = "speakeasy-api/tap/cli"

	// legacyHomebrewFormula is the Homebrew formula that installs the same
	// binary under the legacy gram command name.
	legacyHomebrewFormula = "speakeasy-api/tap/gram"

	// npmPackage is the npm package that installs the speakeasy command.
	npmPackage = "@speakeasy-api/cli"
)

type installMethod string

const (
	installMethodHomebrew installMethod = "homebrew"
	installMethodAqua     installMethod = "aqua"
	installMethodNPM      installMethod = "npm"
	installMethodPNPM     installMethod = "pnpm"
	installMethodYarn     installMethod = "yarn"

	// installMethodNodeModules is a node_modules directory that is not a
	// known global root, such as a project dependency or an npx cache.
	installMethodNodeModules installMethod = "node_modules"
	installMethodManual      installMethod = "manual"
)

// nodeGlobalRootTimeout bounds each package manager query for its global
// root. They answer in well under a second; a hung shim must not block update.
const nodeGlobalRootTimeout = 10 * time.Second

// nodeGlobalRoots holds the global package directories that npm, pnpm and
// yarn report. An empty field means that manager is missing or failed.
type nodeGlobalRoots struct {
	// NPM is the output of `npm root -g`.
	NPM string

	// PNPM is the output of `pnpm root -g`.
	PNPM string

	// Yarn is the output of `yarn global dir`.
	Yarn string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func newUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update the speakeasy CLI to the latest version",
		Description: `Update the speakeasy CLI to the latest available version.

This command supports multiple installation methods:
  - Homebrew (macOS/Linux): Automatically runs 'brew upgrade speakeasy-api/tap/cli'
    ('brew upgrade speakeasy-api/tap/gram' when run as the legacy gram command)
  - npm: Automatically runs 'npm install -g @speakeasy-api/cli@latest'
  - pnpm or yarn global installs: Prints the command to run
  - Aqua: Automatically runs 'aqua upgrade speakeasy-api/gram/gram'
  - Manual installation: Downloads and replaces the current binary

The command will detect your installation method and use the appropriate update mechanism.`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "check",
				Aliases: []string{"c"},
				Usage:   "Only check for updates without installing",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Force update even if already on latest version",
			},
		},
		Action: doUpdate,
	}
}

func doUpdate(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)

	// Get current version
	currentVersion := Version
	if currentVersion == "dev" {
		logger.InfoContext(ctx, "Running development build, skipping update check")
		fmt.Println("⚠️  You are running a development build of the speakeasy CLI")
		fmt.Println("   Development builds cannot be updated via this command")
		return nil
	}

	// Fetch latest release information
	logger.DebugContext(ctx, "fetching latest release info from GitHub")
	latestRelease, err := fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	// Parse versions
	current, err := semver.NewVersion(currentVersion)
	if err != nil {
		return fmt.Errorf("failed to parse current version %q: %w", currentVersion, err)
	}

	// Strip "cli@" or "cli/" prefix from tag name
	latestVersionStr := strings.TrimPrefix(latestRelease.TagName, "cli@")
	latestVersionStr = strings.TrimPrefix(latestVersionStr, "cli/")
	latest, err := semver.NewVersion(latestVersionStr)
	if err != nil {
		return fmt.Errorf("failed to parse latest version %q: %w", latestVersionStr, err)
	}

	// Check if update is needed
	if current.GreaterThan(latest) || current.Equal(latest) {
		if !c.Bool("force") {
			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
			fmt.Println(successStyle.Render(fmt.Sprintf("✓ You are already on the latest version: %s", currentVersion)))
			return nil
		}
		logger.InfoContext(ctx, "forcing update even though on latest version")
	}

	// If --check flag is set, just report and exit
	if c.Bool("check") {
		infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
		fmt.Println(infoStyle.Render(fmt.Sprintf("→ Update available: %s → %s", currentVersion, latestVersionStr)))
		return nil
	}

	// Detect installation method
	method := detectInstallMethod(logger, ctx)
	logger.InfoContext(ctx, "detected installation method", slog.String("method", string(method)))

	// Perform update based on installation method
	switch method {
	case installMethodHomebrew:
		return updateViaHomebrew(ctx, logger)
	case installMethodNPM:
		return updateViaNPM(ctx, logger)
	case installMethodPNPM, installMethodYarn, installMethodNodeModules:
		fmt.Println(nodePackageManagerHint(method))
		return nil
	case installMethodAqua:
		return updateViaAqua(ctx, logger)
	case installMethodManual:
		return updateManual(ctx, logger, latestRelease, latestVersionStr)
	default:
		return fmt.Errorf("unknown installation method")
	}
}

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleasesAPI, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "gram-cli")

	client := api.SharedHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release info: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode releases: %w", err)
	}

	// Find the latest CLI release (tags starting with "cli@" or "cli/")
	for _, release := range releases {
		if strings.HasPrefix(release.TagName, "cli@") || strings.HasPrefix(release.TagName, "cli/") {
			return &release, nil
		}
	}

	return nil, fmt.Errorf("no CLI releases found")
}

func detectInstallMethod(logger *slog.Logger, ctx context.Context) installMethod {
	exePath, err := os.Executable()
	if err != nil {
		logger.DebugContext(ctx, "failed to get executable path", slog.String("error", err.Error()))
		return installMethodManual
	}

	// Resolve symlinks to get the real path
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		logger.DebugContext(ctx, "failed to resolve symlinks", slog.String("error", err.Error()))
		return installMethodManual
	}

	var roots nodeGlobalRoots
	if strings.Contains(normalizePath(realPath), "/node_modules/") {
		roots = nodeGlobalRoots{
			NPM:  commandPath(ctx, "npm", "root", "-g"),
			PNPM: commandPath(ctx, "pnpm", "root", "-g"),
			Yarn: commandPath(ctx, "yarn", "global", "dir"),
		}
	}

	method := installMethodForPath(realPath, roots)
	logger.DebugContext(ctx, "resolved executable path", slog.String("path", realPath), slog.String("method", string(method)))
	return method
}

// commandPath runs a package manager query and returns the directory it
// prints, with symlinks resolved. It returns "" when the command fails.
func commandPath(ctx context.Context, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, nodeGlobalRootTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // callers pass fixed package manager commands
	if err != nil {
		return ""
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// normalizePath uses forward slashes so path checks work on every OS.
func normalizePath(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}

// isUnder reports whether path is inside dir. Windows paths are not case
// sensitive, so the comparison ignores case.
func isUnder(path, dir string) bool {
	if dir == "" {
		return false
	}
	prefix := strings.TrimSuffix(normalizePath(dir), "/") + "/"
	return len(path) > len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
}

// installMethodForPath infers how the CLI was installed from the resolved path
// of its executable and the global roots of the Node package managers.
func installMethodForPath(realPath string, roots nodeGlobalRoots) installMethod {
	p := normalizePath(realPath)

	// Check the Node package managers before Homebrew, because a
	// Homebrew-installed Node keeps its global packages under the Homebrew
	// prefix.
	switch {
	case isUnder(p, roots.NPM):
		return installMethodNPM
	case isUnder(p, roots.PNPM):
		return installMethodPNPM
	case isUnder(p, roots.Yarn):
		return installMethodYarn
	case strings.Contains(p, "/node_modules/"):
		return installMethodNodeModules
	}

	// Homebrew typically installs to /usr/local/Cellar, /opt/homebrew/Cellar, or /home/linuxbrew
	for _, marker := range []string{"/Cellar/", "/homebrew/"} {
		if strings.Contains(p, marker) {
			return installMethodHomebrew
		}
	}

	// Aqua typically installs to ~/.local/share/aquaproj-aqua/
	if strings.Contains(p, "aquaproj-aqua") {
		return installMethodAqua
	}

	return installMethodManual
}

// nodePackageManagerHint tells the user how to update an install that a Node
// package manager owns but this command does not update itself.
func nodePackageManagerHint(method installMethod) string {
	switch method {
	case installMethodPNPM:
		return fmt.Sprintf("Installed with pnpm. Update with: pnpm add -g %s@latest", npmPackage)
	case installMethodYarn:
		return fmt.Sprintf("Installed with yarn. Update with: yarn global add %s@latest", npmPackage)
	case installMethodNodeModules:
		return fmt.Sprintf("Installed inside a node_modules directory that is not a global npm, pnpm or yarn root. Update %s with the package manager that installed it.", npmPackage)
	case installMethodHomebrew, installMethodAqua, installMethodNPM, installMethodManual:
		return ""
	}
	return ""
}

// homebrewFormulaFor returns the formula that installed the CLI, based on the
// command name it was run as.
func homebrewFormulaFor(arg0 string) string {
	if invokedAs(arg0) == legacyCommandName {
		return legacyHomebrewFormula
	}
	return homebrewFormula
}

// versionHint tells the user how to check the version they now have, using
// the command name they ran.
func versionHint() string {
	name := commandName
	if invokedAs(os.Args[0]) == legacyCommandName {
		name = legacyCommandName
	}
	return fmt.Sprintf("  Run '%s --version' to verify the new version", name)
}

func updateViaHomebrew(ctx context.Context, logger *slog.Logger) error {
	fmt.Println("🍺 Updating via Homebrew...")

	cmd := exec.CommandContext(ctx, "brew", "upgrade", homebrewFormulaFor(os.Args[0])) //nolint:gosec // the formula is one of two constants
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew upgrade failed: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	fmt.Println(successStyle.Render("✓ Successfully updated the speakeasy CLI via Homebrew"))
	fmt.Println(versionHint())
	return nil
}

func updateViaNPM(ctx context.Context, logger *slog.Logger) error {
	fmt.Println("📦 Updating via npm...")

	cmd := exec.CommandContext(ctx, "npm", "install", "-g", npmPackage+"@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm install failed: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	fmt.Println(successStyle.Render("✓ Successfully updated the speakeasy CLI via npm"))
	fmt.Println(versionHint())
	return nil
}

func updateViaAqua(ctx context.Context, logger *slog.Logger) error {
	fmt.Println("💧 Updating via Aqua...")

	cmd := exec.CommandContext(ctx, "aqua", "upgrade", "speakeasy-api/gram/gram")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("aqua upgrade failed: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	fmt.Println(successStyle.Render("✓ Successfully updated the speakeasy CLI via Aqua"))
	fmt.Println(versionHint())
	return nil
}

func updateManual(ctx context.Context, logger *slog.Logger, release *githubRelease, version string) error {
	fmt.Printf("📦 Updating manually to version %s...\n", version)

	// Determine the asset name for the current platform
	assetName := fmt.Sprintf("%s_%s_%s.zip", commandName, runtime.GOOS, runtime.GOARCH)
	logger.DebugContext(ctx, "looking for asset", slog.String("name", assetName))

	// Find the matching asset
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, version)
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get the actual binary location
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	logger.InfoContext(ctx, "downloading update",
		slog.String("url", downloadURL),
		slog.String("target", realPath))

	// Download the new binary
	tmpFile, err := downloadBinary(ctx, downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer func() {
		_ = os.Remove(tmpFile)
	}()

	// Replace the current binary
	if err := replaceBinary(realPath, tmpFile); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	fmt.Println(successStyle.Render(fmt.Sprintf("✓ Successfully updated the speakeasy CLI to version %s", version)))
	fmt.Println(versionHint())
	return nil
}

func downloadBinary(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	client := api.SharedHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "speakeasy-update-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		_ = tmpFile.Close()
	}()

	// Write the downloaded content (with size limit to prevent decompression bombs)
	limitedReader := io.LimitReader(resp.Body, 500*1024*1024) // 500MB limit
	if _, err := io.Copy(tmpFile, limitedReader); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write download: %w", err)
	}

	return tmpFile.Name(), nil
}

func replaceBinary(targetPath, zipPath string) error {
	// Extract the binary from the zip file
	binaryPath, err := extractBinaryFromZip(zipPath)
	if err != nil {
		return fmt.Errorf("failed to extract binary from zip: %w", err)
	}
	defer func() {
		_ = os.Remove(binaryPath)
	}()

	// Read the new binary
	// #nosec G304 - binaryPath is from our controlled extraction, not user input
	newBinary, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to read new binary: %w", err)
	}

	// Get the target file permissions
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("failed to stat target binary: %w", err)
	}

	// Create a backup of the current binary
	backupPath := targetPath + ".backup"
	if err := os.Rename(targetPath, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Write the new binary
	// #nosec G304 - targetPath is the current executable path, not user input
	if err := os.WriteFile(targetPath, newBinary, targetInfo.Mode()); err != nil {
		// Restore backup on failure
		_ = os.Rename(backupPath, targetPath)
		return fmt.Errorf("failed to write new binary: %w", err)
	}

	// Remove the backup on success
	_ = os.Remove(backupPath)

	return nil
}

func extractBinaryFromZip(zipPath string) (string, error) {
	// Open the zip file
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	// Find the speakeasy binary in the zip
	binaryName := commandName
	if runtime.GOOS == "windows" {
		binaryName = commandName + ".exe"
	}

	for _, file := range reader.File {
		// Check if this is the binary we're looking for
		if filepath.Base(file.Name) == binaryName {
			// Open the file in the zip
			rc, err := file.Open()
			if err != nil {
				return "", fmt.Errorf("failed to open file in zip: %w", err)
			}
			defer func() {
				_ = rc.Close()
			}()

			// Create a temporary file for the extracted binary
			tmpFile, err := os.CreateTemp("", "speakeasy-binary-*")
			if err != nil {
				return "", fmt.Errorf("failed to create temp file: %w", err)
			}
			defer func() {
				_ = tmpFile.Close()
			}()

			// Copy the binary content with size limit to prevent decompression bombs
			// #nosec G110 - Size limit prevents decompression bomb attacks
			limitedReader := io.LimitReader(rc, 200*1024*1024) // 200MB limit for binary
			if _, err := io.Copy(tmpFile, limitedReader); err != nil {
				_ = os.Remove(tmpFile.Name())
				return "", fmt.Errorf("failed to extract binary: %w", err)
			}

			return tmpFile.Name(), nil
		}
	}

	return "", fmt.Errorf("binary %q not found in zip archive", binaryName)
}
