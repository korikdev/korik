package autoupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var githubReleasesURL = "https://api.github.com/repos/korikdev/korik/releases/latest"

const cacheDuration = 24 * time.Hour

// UpdateInfo holds information about a available update.
type UpdateInfo struct {
	Version        string
	ReleasedAt     time.Time
	NotesURL       string
	CurrentVersion string
	HasUpdate      bool
}

// cacheRecord wraps UpdateInfo with a timestamp so the expiry check
// uses the actual moment the result was stored rather than program start time.
type cacheRecord struct {
	Info     UpdateInfo `json:"info"`
	CachedAt time.Time  `json:"cached_at"`
}

// CheckResult holds the result of an update check.
type CheckResult struct {
	UpdateInfo UpdateInfo
	Cached     bool
	Error      string
}

// Checker manages update checking with caching and async support.
type Checker struct {
	currentVersion string
	cacheDir       string
	cachePath      string
	noCheck        bool
	mu             sync.RWMutex
	cached         *UpdateInfo
	cachedTime     time.Time
}

// NewChecker creates a new update checker with the given current version and cache directory.
func NewChecker(currentVersion, cacheDir string) *Checker {
	c := &Checker{
		currentVersion: currentVersion,
		cacheDir:       cacheDir,
		cachePath:      filepath.Join(cacheDir, "update_cache.json"),
	}
	c.loadCache()
	return c
}

// SetNoCheck disables update checking when noCheck is true.
func (c *Checker) SetNoCheck(noCheck bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noCheck = noCheck
}

// GetCached returns the cached update info if available and not expired.
func (c *Checker) GetCached() (*UpdateInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cached == nil {
		return nil, false
	}
	if time.Since(c.cachedTime) > cacheDuration {
		return nil, false
	}
	info := *c.cached
	return &info, true
}

// CheckForUpdate queries the GitHub releases API for the latest version.
// It returns an error if the check fails or update checking is disabled.
func (c *Checker) CheckForUpdate() (*UpdateInfo, error) {
	c.mu.RLock()
	if c.noCheck {
		c.mu.RUnlock()
		return nil, nil
	}
	c.mu.RUnlock()

	if ui, ok := c.GetCached(); ok {
		return ui, nil
	}

	info, err := c.fetchLatest()
	if err != nil {
		return nil, err
	}

	c.saveCache(info)
	return info, nil
}

// ForceCheck performs an immediate update check, bypassing the cache.
func (c *Checker) ForceCheck() (*UpdateInfo, error) {
	c.mu.RLock()
	if c.noCheck {
		c.mu.RUnlock()
		return nil, nil
	}
	c.mu.RUnlock()

	info, err := c.fetchLatest()
	if err != nil {
		return nil, err
	}

	c.saveCache(info)
	return info, nil
}

// CheckAsync performs an update check in the background and returns a channel
// that will receive the result. The check is non-blocking.
func (c *Checker) CheckAsync() <-chan *CheckResult {
	ch := make(chan *CheckResult, 1)
	go func() {
		defer close(ch)
		info, err := c.CheckForUpdate()
		if err != nil {
			ch <- &CheckResult{Error: err.Error()}
			return
		}
		ch <- &CheckResult{UpdateInfo: *info}
	}()
	return ch
}

// fetchLatest queries the GitHub API and parses the response.
func (c *Checker) fetchLatest() (*UpdateInfo, error) {
	resp, err := http.Get(githubReleasesURL)
	if err != nil {
		return nil, fmt.Errorf("update check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check failed: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("update check failed: reading response: %w", err)
	}

	var release struct {
		TagName     string `json:"tag_name"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("update check failed: parsing response: %w", err)
	}

	releasedAt, err := time.Parse(time.RFC3339, release.PublishedAt)
	if err != nil {
		releasedAt = time.Time{}
	}

	info := &UpdateInfo{
		Version:        release.TagName,
		ReleasedAt:     releasedAt,
		NotesURL:       release.HTMLURL,
		CurrentVersion: c.currentVersion,
		HasUpdate:      release.TagName != "" && release.TagName != c.currentVersion,
	}

	return info, nil
}

// saveCache persists the update info to disk together with the storage
// timestamp so loadCache can restore the original cachedTime on reload.
func (c *Checker) saveCache(info *UpdateInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.cached = info
	c.cachedTime = now

	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return
	}

	record := cacheRecord{Info: *info, CachedAt: now}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.cachePath, data, 0o600)
}

// loadCache restores the cached update info from disk if it exists and is not expired.
// It reads the stored CachedAt timestamp to evaluate freshness correctly,
// avoiding the bug where c.cachedTime is zero and every on-disk cache appears expired.
func (c *Checker) loadCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.cachePath)
	if err != nil {
		return
	}

	// Attempt to parse the new cacheRecord format (with CachedAt timestamp).
	var record cacheRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return
	}

	// If the stored record has no CachedAt (legacy format), skip it.
	if record.CachedAt.IsZero() {
		return
	}

	// Validate expiry against the actual cache creation time from disk.
	if time.Since(record.CachedAt) > cacheDuration {
		return
	}

	info := record.Info
	c.cached = &info
	c.cachedTime = record.CachedAt
}
