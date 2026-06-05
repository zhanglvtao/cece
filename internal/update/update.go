package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	repoURL  = "https://api.github.com/repos/zhanglvtao/cece/releases/latest"
	userAgent = "cece"
)

// Default is the default Client.
var Default Client = &github{}

// Info contains information about an available update.
type Info struct {
	Current string
	Latest  string
	URL     string
}

// Matches a version string like v0.0.0-0.20251231235959-06c807842604
var goInstallRegexp = regexp.MustCompile(`^v?\d+\.\d+\.\d+-\d+\.\d{14}-[0-9a-f]{12}$`)

// IsDevelopment returns true if the current version is a development build.
func (i Info) IsDevelopment() bool {
	return i.Current == "devel" || i.Current == "unknown" ||
		strings.Contains(i.Current, "dirty") ||
		goInstallRegexp.MatchString(i.Current)
}

// Available returns true if there's an update available.
func (i Info) Available() bool {
	cpr := strings.Contains(i.Current, "-")
	lpr := strings.Contains(i.Latest, "-")
	if cpr && !lpr {
		return true
	}
	if lpr && !cpr {
		return false
	}
	return i.Current != i.Latest
}

// Check checks if a new version is available.
func Check(ctx context.Context, current string) (Info, error) {
	return CheckWithClient(ctx, current, Default)
}

// CheckWithClient checks if a new version is available using the given client.
func CheckWithClient(ctx context.Context, current string, client Client) (Info, error) {
	info := Info{
		Current: current,
		Latest:  current,
	}

	release, err := client.Latest(ctx)
	if err != nil {
		return info, fmt.Errorf("failed to fetch latest release: %w", err)
	}

	info.Latest = strings.TrimPrefix(release.TagName, "v")
	info.Current = strings.TrimPrefix(info.Current, "v")
	info.URL = release.HTMLURL
	return info, nil
}

// Release represents a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// SelfUpdate downloads and replaces the current binary with the latest release.
// It returns the new version string on success.
func SelfUpdate(ctx context.Context, info Info) (string, error) {
	// Determine asset name for current platform.
	assetName := platformAssetName()
	if assetName == "" {
		return "", fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Get the release with asset details.
	client := Default
	release, err := client.Latest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch release: %w", err)
	}

	downloadURL := ""
	for _, a := range release.Assets {
		if a.Name == assetName+".tar.gz" {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return "", fmt.Errorf("no asset found for %s.tar.gz", assetName)
	}

	// Download and extract.
	bin, err := downloadAndExtract(ctx, downloadURL)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Get current executable path.
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot find current executable: %w", err)
	}

	// Write new binary to a temp file in the same directory.
	// Using the same directory avoids cross-device rename issues.
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "cece-*.new")
	if err != nil {
		// Likely a permission error; try user temp dir instead.
		tmp, err = os.CreateTemp("", "cece-*.new")
		if err != nil {
			return "", fmt.Errorf("create temp file failed: %w", err)
		}
	}
	tmpName := tmp.Name()
	if err := os.Chmod(tmpName, 0o755); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("chmod temp file failed: %w", err)
	}
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("write temp file failed: %w", err)
	}
	tmp.Close()

	// Rename old binary out of the way (inode stays alive until process exits).
	backup := exe + ".old"
	if err := os.Rename(exe, backup); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("permission denied, try: sudo cece update")
	}

	// Rename new binary into place.
	if err := os.Rename(tmpName, exe); err != nil {
		// Try to restore backup.
		os.Rename(backup, exe)
		os.Remove(tmpName)
		return "", fmt.Errorf("install new binary failed: %w", err)
	}

	// Clean up old backup (best effort, may fail if in use).
	os.Remove(backup)

	return info.Latest, nil
}

// platformAssetName returns the asset name for the current platform (without .tar.gz extension).
func platformAssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	switch os {
	case "darwin", "linux":
	default:
		return ""
	}

	switch arch {
	case "amd64":
		return fmt.Sprintf("cece-%s-x64", os)
	case "arm64":
		return fmt.Sprintf("cece-%s-arm64", os)
	default:
		return ""
	}
}

func downloadAndExtract(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	hc := &http.Client{Timeout: 5 * time.Minute}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download returned status %d: %s", resp.StatusCode, string(body))
	}

	return extractFromTarGz(resp.Body)
}

func extractFromTarGz(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar reader: %w", err)
		}

		base := filepath.Base(hdr.Name)
		// The binary is named "cece" or "cece-{os}-{arch}" inside the tarball.
		if base == "cece" || strings.HasPrefix(base, "cece-") {
			return io.ReadAll(tr)
		}
	}

	return nil, fmt.Errorf("binary not found in tarball")
}

// Client is a client that can get the latest release.
type Client interface {
	Latest(ctx context.Context) (*Release, error)
}

type github struct{}

// Latest implements Client.
func (c *github) Latest(ctx context.Context) (*Release, error) {
	hc := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequestWithContext(ctx, "GET", repoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// TarballContent is the content of a tarball entry.
type TarballContent struct {
	Name string
	Data []byte
}

// ListTarball extracts all files from a tar.gz reader.
func ListTarball(r io.Reader) ([]TarballContent, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	var entries []TarballContent
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			entries = append(entries, TarballContent{Name: hdr.Name, Data: data})
		}
	}
	return entries, nil
}

// githubToken returns a GitHub API token from GITHUB_TOKEN env or gh CLI.
func githubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}