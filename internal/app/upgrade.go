package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repo = "Hitesh-K-Murali/terminal-code"

// RunUpgrade checks for a new version and performs an atomic self-update.
func RunUpgrade(currentVersion string) error {
	if currentVersion == "dev" || currentVersion == "" {
		fmt.Println("  You're running a development build.")
		fmt.Println("  To upgrade, rebuild from source or use the install script:")
		fmt.Printf("  curl -fsSL https://raw.githubusercontent.com/%s/main/install.sh | sh\n", repo)
		return nil
	}

	fmt.Printf("  Current version: %s\n", currentVersion)
	fmt.Print("  Checking for updates... ")

	latest, assets, err := fetchLatestRelease()
	if err != nil {
		return fmt.Errorf("check update: %w", err)
	}

	fmt.Printf("%s\n", latest)

	if strings.TrimPrefix(latest, "v") == strings.TrimPrefix(currentVersion, "v") {
		fmt.Println("  Already up to date.")
		return nil
	}

	// Find the right binary
	assetName := fmt.Sprintf("tc-%s-%s", runtime.GOOS, runtime.GOARCH)
	downloadURL := ""
	checksumURL := ""
	for _, a := range assets {
		if a.Name == assetName {
			downloadURL = a.URL
		}
		if a.Name == "checksums.txt" {
			checksumURL = a.URL
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, latest)
	}

	// Get current binary path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current binary: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve symlinks: %w", err)
	}

	// Check we can write to the binary location
	dir := filepath.Dir(execPath)
	if !isWritable(dir) {
		return fmt.Errorf("cannot write to %s — try running with sudo, or reinstall to ~/.local/bin", dir)
	}

	// Download new binary to temp file in SAME directory (for atomic rename)
	fmt.Printf("  Downloading %s... ", assetName)
	tmpFile, err := os.CreateTemp(dir, "tc-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up on failure

	resp, err := http.Get(downloadURL)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		tmpFile.Close()
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)
	n, err := io.Copy(writer, resp.Body)
	tmpFile.Close()
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	fmt.Printf("done (%d MB)\n", n/(1024*1024))

	// Verify checksum
	if checksumURL != "" {
		fmt.Print("  Verifying checksum... ")
		if err := verifyChecksum(checksumURL, assetName, actualHash); err != nil {
			return fmt.Errorf("checksum: %w", err)
		}
		fmt.Println("verified")
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic swap
	fmt.Print("  Installing... ")
	if err := os.Rename(tmpPath, execPath); err != nil {
		return fmt.Errorf("replace binary: %w (try reinstalling to a writable location)", err)
	}
	fmt.Println("done")

	fmt.Printf("\n  Upgraded: %s → %s\n", currentVersion, latest)
	return nil
}

type releaseAsset struct {
	Name string
	URL  string
}

func fetchLatestRelease() (version string, assets []releaseAsset, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo))
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", nil, err
	}

	for _, a := range release.Assets {
		assets = append(assets, releaseAsset{Name: a.Name, URL: a.BrowserDownloadURL})
	}

	return release.TagName, assets, nil
}

func verifyChecksum(checksumURL, assetName, actualHash string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(checksumURL)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("asset %s not found in checksums", assetName)
}

func isWritable(dir string) bool {
	tmp, err := os.CreateTemp(dir, ".tc-write-test-*")
	if err != nil {
		return false
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return true
}
