package antigravity

// Keeps the reported Antigravity client version current.
//
// Google's Cloud Code backend gates model availability on the client version and
// family in the User-Agent: clients below 2.9.0 are refused newer models, and a
// User-Agent without the "hub/" family segment has been observed to get 404 for
// some models (upstream issue #6419). A version pinned in the source therefore
// rots: it silently loses access to models as Google moves forward.
//
// This fetches the version from the same manifest the official desktop updater
// reads, caches it, and refreshes it in the background. Every failure falls back
// to the compiled-in default, so a blocked network only means a stale version,
// never a broken request.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// hubLatestManifestURL is the manifest published for the official desktop
	// client's auto-updater. It is a small YAML document whose first key is the
	// version, e.g. "version: 2.22.0".
	hubLatestManifestURL = "https://antigravity-hub-auto-updater-974169037036.us-central1.run.app/manifest/latest-arm64-mac.yml"

	// HubPlatform is the platform token reported in the User-Agent. The real
	// clients report their own platform and Google accepts any of them; a single
	// fixed value keeps every deployment identical regardless of server OS.
	HubPlatform = "darwin/arm64"

	hubFetchTimeout  = 10 * time.Second
	hubRefreshPeriod = 6 * time.Hour
	hubRetryPeriod   = 15 * time.Minute
)

var hubManifestVersionPattern = regexp.MustCompile(`(?m)^\s*version\s*:\s*["']?([0-9]+\.[0-9]+\.[0-9]+)["']?\s*$`)

var (
	hubVersionMu      sync.RWMutex
	hubFetchedVersion string
	hubUpdaterOnce    sync.Once
)

// FetchedHubVersion returns the most recently fetched client version, or "" when
// no successful fetch has happened yet.
func FetchedHubVersion() string {
	hubVersionMu.RLock()
	defer hubVersionMu.RUnlock()
	return hubFetchedVersion
}

func setFetchedHubVersion(version string) {
	hubVersionMu.Lock()
	hubFetchedVersion = version
	hubVersionMu.Unlock()
}

// StartHubVersionUpdater starts the background refresh exactly once. It is safe
// to call from any code path; callers do not need to coordinate.
func StartHubVersionUpdater() {
	hubUpdaterOnce.Do(func() {
		go func() {
			for {
				wait := hubRefreshPeriod
				ctx, cancel := context.WithTimeout(context.Background(), hubFetchTimeout)
				version, err := fetchHubLatestVersion(ctx)
				cancel()
				if err == nil && version != "" {
					setFetchedHubVersion(version)
				} else if FetchedHubVersion() == "" {
					// Nothing cached yet: retry sooner so a transient failure at
					// startup does not pin the default version for six hours.
					wait = hubRetryPeriod
				}
				time.Sleep(wait)
			}
		}()
	})
}

func fetchHubLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubLatestManifestURL, nil)
	if err != nil {
		return "", err
	}
	// The manifest is served to the desktop updater; mirror its request headers.
	req.Header.Set("User-Agent", "electron-builder")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := (&http.Client{Timeout: hubFetchTimeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch antigravity hub manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("antigravity hub manifest returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	match := hubManifestVersionPattern.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("no version found in antigravity hub manifest")
	}
	return strings.TrimSpace(string(match[1])), nil
}
