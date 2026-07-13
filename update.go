package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Self-update constraints. Downloads are restricted to the mowa repository on
// GitHub over HTTPS; no URL is ever accepted from the request body. Every zip
// is verified against the release's checksums.txt before it is installed.
const (
	updateRepoOwner    = "mauromorales"
	updateRepoName     = "mowa"
	githubAPIBase      = "https://api.github.com"
	githubDownloadHost = "github.com"
	githubAPIHost      = "api.github.com"

	// updateHTTPTimeout bounds each GitHub request/download. Releases are a
	// few MB, so a generous minute is plenty while still failing eventually.
	updateHTTPTimeout = 60 * time.Second

	// checksumsAssetName is the goreleaser checksums file shipped with every release.
	checksumsAssetName = "checksums.txt"

	// binaryNameInZip is the executable's name inside each release archive.
	binaryNameInZip = "mowa"

	// restartDelay gives the HTTP response a moment to flush before the process
	// exits and (under launchd KeepAlive) is relaunched on the new binary.
	restartDelay = 500 * time.Millisecond
)

// githubRelease is the subset of the GitHub release API we consume.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// @Summary Update mowa to a release and restart
// @Description Downloads a release from https://github.com/mauromorales/mowa/releases, verifies its sha256 checksum, replaces the running binary in place, and exits so the process is relaunched on the new version.
// @Description
// @Description Restart relies on the launchd service (KeepAlive) relaunching the process after it exits 0. If mowa is not running under launchd, the process simply exits after updating and must be started again manually.
// @Description
// @Description The version may be given with or without a leading "v". Omit it to install the latest release. Requesting the already-installed version is a no-op on release builds; local ("dev") builds always reinstall.
// @Tags system
// @Accept json
// @Produce json
// @Param request body UpdateRequest false "Update request (empty body installs the latest release)"
// @Success 200 {object} UpdateResponse "Update applied (process is exiting) or already up to date"
// @Failure 400 {object} UpdateResponse "Bad request - invalid body or unsupported architecture"
// @Failure 404 {object} UpdateResponse "Requested version not found"
// @Failure 500 {object} UpdateResponse "Update failed (e.g. checksum mismatch); running binary untouched"
// @Failure 502 {object} UpdateResponse "Failed to reach GitHub"
// @Router /api/update [post]
func handleUpdate(c echo.Context) error {
	var req UpdateRequest
	// An empty body is valid (install the latest release). Echo's binder
	// surfaces that as io.EOF, so treat only EOF as "no body" and reject
	// everything else as malformed.
	if err := c.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		return c.JSON(http.StatusBadRequest, UpdateResponse{
			Success: false,
			Message: "invalid request body",
			Error:   err.Error(),
		})
	}

	// Determine which release asset matches this machine before hitting the
	// network, so an unsupported OS/architecture fails fast with a clear error.
	assetName, err := assetNameForArch(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return c.JSON(http.StatusBadRequest, UpdateResponse{
			Success: false,
			Message: "unsupported platform",
			Error:   err.Error(),
		})
	}

	// Resolve the target release (explicit tag or latest).
	release, status, err := resolveRelease(req.Version)
	if err != nil {
		return c.JSON(status, UpdateResponse{
			Success:         false,
			PreviousVersion: normalizeVersion(version),
			Message:         "could not resolve release",
			Error:           err.Error(),
		})
	}

	targetVersion := normalizeVersion(release.TagName)
	currentVersion := normalizeVersion(version)

	// Short-circuit if already on the requested version. Skipped for dev builds
	// whose version string is not a real release.
	if version != "dev" && targetVersion == currentVersion {
		return c.JSON(http.StatusOK, UpdateResponse{
			Success:          false,
			PreviousVersion:  currentVersion,
			InstalledVersion: currentVersion,
			Message:          fmt.Sprintf("already up to date (version %s)", currentVersion),
		})
	}

	// Locate the architecture-matching zip and the checksums file.
	zipAsset, err := findAsset(release, assetName)
	if err != nil {
		return c.JSON(http.StatusNotFound, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "release is missing the expected asset",
			Error:           err.Error(),
		})
	}
	checksumsAsset, err := findAsset(release, checksumsAssetName)
	if err != nil {
		return c.JSON(http.StatusNotFound, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "release is missing checksums.txt",
			Error:           err.Error(),
		})
	}

	// Download the zip and checksums, then verify the zip before touching disk.
	zipData, err := downloadReleaseAsset(zipAsset.BrowserDownloadURL)
	if err != nil {
		return c.JSON(http.StatusBadGateway, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "failed to download release asset",
			Error:           err.Error(),
		})
	}
	checksumsData, err := downloadReleaseAsset(checksumsAsset.BrowserDownloadURL)
	if err != nil {
		return c.JSON(http.StatusBadGateway, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "failed to download checksums.txt",
			Error:           err.Error(),
		})
	}

	if err := verifyChecksum(zipData, string(checksumsData), assetName); err != nil {
		return c.JSON(http.StatusInternalServerError, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "checksum verification failed; binary not replaced",
			Error:           err.Error(),
		})
	}

	// Extract the binary and atomically replace the running executable.
	binaryData, err := extractBinaryFromZip(zipData)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "failed to extract binary from release archive",
			Error:           err.Error(),
		})
	}

	if err := replaceRunningBinary(binaryData); err != nil {
		return c.JSON(http.StatusInternalServerError, UpdateResponse{
			Success:         false,
			PreviousVersion: currentVersion,
			Message:         "failed to install new binary",
			Error:           err.Error(),
		})
	}

	// Success. Reply, then exit so launchd relaunches the new binary.
	resp := UpdateResponse{
		Success:          true,
		PreviousVersion:  currentVersion,
		InstalledVersion: targetVersion,
		Message:          fmt.Sprintf("updated from %s to %s; the service is restarting", currentVersion, targetVersion),
	}
	// The binary on disk has already been replaced, so we must restart even if
	// writing the response fails (e.g. the client disconnected). Otherwise the
	// process would keep serving the old in-memory binary indefinitely.
	writeErr := c.JSON(http.StatusOK, resp)
	if writeErr != nil {
		log.Printf("failed to write update response, restarting anyway: %v", writeErr)
	}

	log.Printf("🔄 Updated from %s to %s; exiting to restart on the new binary", currentVersion, targetVersion)
	go func() {
		time.Sleep(restartDelay)
		os.Exit(0)
	}()

	return writeErr
}

// assetNameForArch maps the running OS/architecture to the release zip name.
// Releases only ship Darwin binaries (mowa drives macOS-only tools), so any
// other OS is rejected up front rather than failing later with a misleading
// "asset not found".
func assetNameForArch(goos, goarch string) (string, error) {
	if goos != "darwin" {
		return "", fmt.Errorf("self-update only supports macOS, not %q", goos)
	}
	switch goarch {
	case "arm64":
		return "mowa_Darwin_arm64.zip", nil
	case "amd64":
		return "mowa_Darwin_x86_64.zip", nil
	default:
		return "", fmt.Errorf("no release asset for architecture %q", goarch)
	}
}

// normalizeVersion strips a single leading "v" so "v0.4.2" and "0.4.2" compare equal.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// resolveRelease fetches the target release from the GitHub API. An empty
// version resolves the latest (non-draft, non-prerelease) release. The returned
// int is the HTTP status to surface on error (404 unknown tag, 502 network).
func resolveRelease(requestedVersion string) (*githubRelease, int, error) {
	var apiURL string
	if strings.TrimSpace(requestedVersion) == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, updateRepoOwner, updateRepoName)
	} else {
		tag := "v" + normalizeVersion(strings.TrimSpace(requestedVersion))
		apiURL = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBase, updateRepoOwner, updateRepoName, tag)
	}

	body, status, err := githubAPIGet(apiURL)
	if err != nil {
		if status == http.StatusNotFound {
			return nil, http.StatusNotFound, fmt.Errorf("release not found: %s", requestedVersion)
		}
		return nil, http.StatusBadGateway, err
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("failed to parse GitHub release response: %w", err)
	}
	if release.TagName == "" {
		return nil, http.StatusBadGateway, fmt.Errorf("GitHub release response had no tag")
	}
	return &release, http.StatusOK, nil
}

// githubAPIGet performs a GET against the GitHub API, enforcing HTTPS and the
// api.github.com host. It returns the body, the HTTP status, and an error for
// any non-2xx response.
func githubAPIGet(rawURL string) ([]byte, int, error) {
	if err := ensureAllowedHost(rawURL, githubAPIHost); err != nil {
		return nil, 0, err
	}

	client := &http.Client{Timeout: updateHTTPTimeout}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "mowa-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to reach GitHub: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // API responses are small
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read GitHub response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

// findAsset returns the named asset from a release.
func findAsset(release *githubRelease, name string) (*githubAsset, error) {
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("asset %q not found in release %s", name, release.TagName)
}

// downloadReleaseAsset downloads an asset, enforcing HTTPS and a GitHub-owned
// host on both the initial URL and every redirect. Asset downloads legitimately
// redirect from github.com to *.githubusercontent.com, so those are allowed
// while any other host aborts the download.
func downloadReleaseAsset(rawURL string) ([]byte, error) {
	if err := ensureGitHubAssetHost(rawURL); err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: updateHTTPTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return ensureGitHubAssetHost(req.URL.String())
		},
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mowa-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download of %s returned %d", rawURL, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // releases are a few MB; cap at 64MB
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", rawURL, err)
	}
	return data, nil
}

// ensureAllowedHost rejects any URL that is not HTTPS on the exact expected host.
func ensureAllowedHost(rawURL, expectedHost string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS URL: %s", rawURL)
	}
	if u.Hostname() != expectedHost {
		return fmt.Errorf("refusing URL with unexpected host %q (want %q)", u.Hostname(), expectedHost)
	}
	return nil
}

// ensureGitHubAssetHost accepts an HTTPS URL on github.com or any
// *.githubusercontent.com host, the hosts GitHub serves release assets from
// (directly or after a redirect). Everything else is refused.
func ensureGitHubAssetHost(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS URL: %s", rawURL)
	}
	host := u.Hostname()
	if host == githubDownloadHost || strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("refusing download from unexpected host %q", host)
}

// verifyChecksum confirms the sha256 of data matches the entry for assetName in
// a goreleaser checksums.txt ("<hex>  <filename>" per line).
func verifyChecksum(data []byte, checksums, assetName string) error {
	want := ""
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		if fields[1] == assetName {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum found for %s", assetName)
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", assetName, got, want)
	}
	return nil
}

// extractBinaryFromZip returns the "mowa" binary's bytes from a release zip.
func extractBinaryFromZip(zipData []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	for _, f := range zr.File {
		if filepath.Base(f.Name) != binaryNameInZip {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open %s in zip: %w", f.Name, err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s from zip: %w", f.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("binary %q not found in release archive", binaryNameInZip)
}

// replaceRunningBinary writes the new binary to a temp file in the same
// directory as the running executable, then atomically renames it over the
// executable's path. Renaming over the path of a running binary is safe on
// macOS; writing into the open file is not.
func replaceRunningBinary(binaryData []byte) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate running executable: %w", err)
	}

	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".mowa-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any failure before the rename succeeds.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(binaryData); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close new binary: %w", err)
	}
	if err := os.Chmod(tmpName, 0755); err != nil {
		return fmt.Errorf("failed to chmod new binary: %w", err)
	}
	if err := os.Rename(tmpName, exePath); err != nil {
		return fmt.Errorf("failed to replace running binary: %w", err)
	}

	success = true
	return nil
}
