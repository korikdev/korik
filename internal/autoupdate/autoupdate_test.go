package autoupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewChecker(t *testing.T) {
	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	if c.currentVersion != "v1.0.0" {
		t.Fatalf("expected currentVersion v1.0.0, got %s", c.currentVersion)
	}
	if c.cacheDir != dir {
		t.Fatalf("expected cacheDir %s, got %s", dir, c.cacheDir)
	}
}

func TestGetCached_NilWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	info, ok := c.GetCached()
	if ok {
		t.Fatal("expected no cached result")
	}
	if info != nil {
		t.Fatal("expected nil info")
	}
}

func TestSetNoCheck(t *testing.T) {
	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	c.SetNoCheck(true)
	c.mu.RLock()
	if !c.noCheck {
		t.Fatal("expected noCheck to be true")
	}
	c.mu.RUnlock()
	c.SetNoCheck(false)
	c.mu.RLock()
	if c.noCheck {
		t.Fatal("expected noCheck to be false")
	}
	c.mu.RUnlock()
}

func TestCheckForUpdate_WithServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	info, err := c.CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Version != "v2.0.0" {
		t.Fatalf("expected version v2.0.0, got %s", info.Version)
	}
	if !info.HasUpdate {
		t.Fatal("expected HasUpdate to be true")
	}
	if info.CurrentVersion != "v1.0.0" {
		t.Fatalf("expected CurrentVersion v1.0.0, got %s", info.CurrentVersion)
	}
	if info.NotesURL == "" {
		t.Fatal("expected NotesURL to be non-empty")
	}
}

func TestCheckForUpdate_NoUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v1.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v1.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	info, err := c.CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.HasUpdate {
		t.Fatal("expected HasUpdate to be false")
	}
}

func TestCheckForUpdate_SilentFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	_, err := c.CheckForUpdate()
	if err == nil {
		t.Fatal("expected error from server failure")
	}
}

func TestCheckAsync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	ch := c.CheckAsync()
	result := <-ch
	if result.Error != "" {
		t.Fatalf("unexpected error in result: %s", result.Error)
	}
	if result.UpdateInfo.Version != "v2.0.0" {
		t.Fatalf("expected version v2.0.0, got %s", result.UpdateInfo.Version)
	}
}

func TestForceCheck_BypassesCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)

	_, err := c.ForceCheck()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cached, ok := c.GetCached()
	if !ok {
		t.Fatal("expected cached result after ForceCheck")
	}
	if cached.Version != "v2.0.0" {
		t.Fatalf("expected cached version v2.0.0, got %s", cached.Version)
	}
}

func TestNoCheck_Disabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when noCheck is true")
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	c.SetNoCheck(true)

	info, err := c.CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatal("expected nil info when noCheck is true")
	}
}

func TestCachePersistence(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update_cache.json")

	info := UpdateInfo{
		Version:        "v2.0.0",
		ReleasedAt:     time.Now(),
		NotesURL:       "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		CurrentVersion: "v1.0.0",
		HasUpdate:      true,
	}

	// Write the cacheRecord format that loadCache expects (includes CachedAt timestamp).
	record := cacheRecord{Info: info, CachedAt: time.Now()}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewChecker("v1.0.0", dir)
	cached, ok := c.GetCached()
	if !ok {
		t.Fatal("expected cached result to be loaded")
	}
	if cached.Version != "v2.0.0" {
		t.Fatalf("expected version v2.0.0, got %s", cached.Version)
	}
}

func TestCheckForUpdate_ForceCheckErrorSilent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)

	resultCh := c.CheckAsync()
	result := <-resultCh
	if result.Error == "" {
		t.Fatal("expected error in result")
	}
}

func TestUpdateInfoFields(t *testing.T) {
	now := time.Now()
	info := UpdateInfo{
		Version:        "v2.0.0",
		ReleasedAt:     now,
		NotesURL:       "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		CurrentVersion: "v1.0.0",
		HasUpdate:      true,
	}
	if info.Version != "v2.0.0" {
		t.Fatal("Version mismatch")
	}
	if !info.HasUpdate {
		t.Fatal("HasUpdate should be true")
	}
	if info.NotesURL == "" {
		t.Fatal("NotesURL should not be empty")
	}
	if !info.ReleasedAt.Equal(now) {
		t.Fatal("ReleasedAt mismatch")
	}
}

func TestCheckResultFields(t *testing.T) {
	info := UpdateInfo{Version: "v2.0.0", HasUpdate: true}
	result := CheckResult{UpdateInfo: info, Cached: false, Error: ""}
	if result.UpdateInfo.Version != "v2.0.0" {
		t.Fatal("UpdateInfo mismatch")
	}
	if result.Cached {
		t.Fatal("Cached should be false")
	}
	if result.Error != "" {
		t.Fatal("Error should be empty")
	}

	errorResult := CheckResult{Error: "some error"}
	if errorResult.Error != "some error" {
		t.Fatal("Error mismatch")
	}
}

func TestCheckerConcurrentAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = c.GetCached()
			_, _ = c.CheckForUpdate()
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestFetchLatest_ParseTime(t *testing.T) {
	layout := time.RFC3339
	publishedAt := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC).Format(layout)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": publishedAt,
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	info, err := c.CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	if !info.ReleasedAt.Equal(expected) {
		t.Fatalf("expected ReleasedAt %v, got %v", expected, info.ReleasedAt)
	}
}

func TestCheckAsync_ChannelCloses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	ch := c.CheckAsync()

	result, ok := <-ch
	if !ok {
		t.Fatal("expected channel to be open")
	}
	if result.Error == "" {
		t.Fatal("expected error in result")
	}

	_, open := <-ch
	if open {
		t.Fatal("expected channel to be closed")
	}
}

func TestCacheExpired(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update_cache.json")

	info := UpdateInfo{
		Version:        "v2.0.0",
		ReleasedAt:     time.Now(),
		NotesURL:       "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		CurrentVersion: "v1.0.0",
		HasUpdate:      true,
	}
	data, _ := json.Marshal(info)
	os.MkdirAll(dir, 0o700)
	os.WriteFile(cachePath, data, 0o600)

	c := NewChecker("v1.0.0", dir)
	c.cachedTime = time.Now().Add(-48 * time.Hour)

	_, ok := c.GetCached()
	if ok {
		t.Fatal("expected expired cache to be unavailable")
	}
}

func TestCheckForUpdate_DoesNotCollectTelemetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent := r.Header.Get("User-Agent")
		if userAgent == "" {
			t.Log("no User-Agent sent (expected, no telemetry)")
		}
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	_, err := c.CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckForUpdate_HandlesInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `not valid json`)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	_, err := c.CheckForUpdate()
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
}

func TestCheckForUpdate_HandlesMissingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	info, err := c.CheckForUpdate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Version != "" {
		t.Fatalf("expected empty version, got %s", info.Version)
	}
	if info.HasUpdate {
		t.Fatal("expected HasUpdate to be false when tag_name is empty")
	}
}

func TestSaveCacheCreatesDir(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "subdir")

	c := NewChecker("v1.0.0", cacheDir)
	info := &UpdateInfo{Version: "v2.0.0", HasUpdate: true}
	c.saveCache(info)

	cached, ok := c.GetCached()
	if !ok {
		t.Fatal("expected cached result after saveCache")
	}
	if cached.Version != "v2.0.0" {
		t.Fatalf("expected version v2.0.0, got %s", cached.Version)
	}
}

func TestCheckForUpdate_NetworkFailure(t *testing.T) {
	c := NewChecker("v1.0.0", t.TempDir())
	_, err := c.CheckForUpdate()
	if err == nil {
		t.Fatal("expected error from network failure")
	}
}

func TestLoadCache_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	cached, ok := c.GetCached()
	if ok || cached != nil {
		t.Fatal("expected no cached result when no cache file exists")
	}
}

func TestLoadCache_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update_cache.json")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(cachePath, []byte("not json"), 0o600)

	c := NewChecker("v1.0.0", dir)
	cached, ok := c.GetCached()
	if ok || cached != nil {
		t.Fatal("expected no cached result when cache file is invalid")
	}
}

func TestAsyncReturnsChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{
			"tag_name":     "v2.0.0",
			"published_at": time.Now().Format(time.RFC3339),
			"html_url":     "https://github.com/korikdev/korik/releases/tag/v2.0.0",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	oldURL := githubReleasesURL
	githubReleasesURL = server.URL + "/releases/latest"
	defer func() { githubReleasesURL = oldURL }()

	dir := t.TempDir()
	c := NewChecker("v1.0.0", dir)
	ch := c.CheckAsync()
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
	result := <-ch
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
