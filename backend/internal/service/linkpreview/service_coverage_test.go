package linkpreview

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPreviewMetadataFallbackAndCacheExpiry(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("User-Agent") != userAgent || !strings.Contains(r.Header.Get("Accept"), "text/html") {
			t.Errorf("request headers = %#v", r.Header)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><head><title>Page &amp; title</title><meta name="description" content="  Description &amp; details  "><link rel="icon" href="../icon.ico"></head></html>`)
	}))
	defer server.Close()

	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	svc := New(server.Client())
	svc.now = func() time.Time { return now }
	want := Preview{URL: server.URL + "/page", Title: "Page & title", Description: "Description & details", FaviconURL: server.URL + "/icon.ico"}
	got, err := svc.Preview(context.Background(), server.URL+"/page")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("preview = %#v, want %#v", got, want)
	}
	if _, err := svc.Preview(context.Background(), server.URL+"/page"); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("hits after cache hit = %d, want 1", hits)
	}
	now = now.Add(successTTL + time.Nanosecond)
	if _, err := svc.Preview(context.Background(), server.URL+"/page"); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("hits after success expiry = %d, want 2", hits)
	}
}

func TestPreviewFailureClassesAreCachedWithShortTTL(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	now := time.Now()
	svc := New(server.Client())
	svc.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		_, err := svc.Preview(context.Background(), server.URL)
		if !errors.Is(err, ErrFetchFailed) {
			t.Fatalf("fetch attempt %d error = %v, want ErrFetchFailed", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cached upstream failure calls = %d, want 1", calls)
	}
	now = now.Add(failureTTL + time.Nanosecond)
	_, err := svc.Preview(context.Background(), server.URL)
	if err == nil || err.Error() != errNotHTML.Error() {
		t.Fatalf("non-HTML response error = %v, want not found", err)
	}
	if calls != 2 {
		t.Fatalf("calls after failure expiry = %d, want 2", calls)
	}
	if _, err := (cacheEntry{failure: failureNotFound}).result(); err == nil || err.Error() != errNotHTML.Error() {
		t.Fatalf("cached not-found result error = %v", err)
	}
	if _, err := (cacheEntry{failure: failureFetch}).result(); !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("cached fetch failure = %v", err)
	}
}

func TestPreviewRedirectUsesFinalURLAndDefaultClientBoundsRedirects(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xhtml+xml")
		_, _ = fmt.Fprint(w, `<head><meta property="og:title" content="Target"><meta property="og:image" content="/image.png"></head>`)
	}))
	defer final.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/landing", http.StatusFound)
	}))
	defer redirect.Close()
	got, err := New(redirect.Client()).Preview(context.Background(), redirect.URL+"/short")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != final.URL+"/landing" || got.Title != "Target" || got.ImageURL != final.URL+"/image.png" || got.FaviconURL != final.URL+"/favicon.ico" {
		t.Fatalf("redirect preview = %#v", got)
	}

	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/next", http.StatusFound)
	}))
	defer loop.Close()
	_, err = New(nil).Preview(context.Background(), loop.URL)
	if !errors.Is(err, ErrFetchFailed) {
		t.Fatalf("default redirect limit error = %v, want ErrFetchFailed", err)
	}
}

func TestPreviewValidationAndURLResolution(t *testing.T) {
	for _, raw := range []string{"", "/relative", "ftp://example.com/file", "https:///missing-host"} {
		if _, err := validateURL(raw); err == nil {
			t.Errorf("validateURL(%q) error = nil", raw)
		}
	}
	if _, err := New(nil).Preview(context.Background(), "not a URL: %"); err == nil {
		t.Fatal("Preview accepted malformed URL")
	}
	base, _ := url.Parse("https://example.com/a/page")
	if got := resolveReference(base, " ../img.png "); got != "https://example.com/img.png" {
		t.Fatalf("relative resolution = %q", got)
	}
	if got := resolveReference(base, "://bad"); got != "" {
		t.Fatalf("invalid reference resolution = %q, want empty", got)
	}
}

func TestCacheEvictsExpiredThenOldestAndUsesFailureTTL(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	svc := New(nil)
	svc.now = func() time.Time { return now }
	if _, ok := svc.lookup("missing"); ok {
		t.Fatal("missing cache key found")
	}
	svc.store("failure", cacheEntry{failure: failureFetch, fetchedAt: now})
	if ttl := entryTTL(failureFetch); ttl != failureTTL {
		t.Fatalf("failure TTL = %v", ttl)
	}
	if _, ok := svc.lookup("failure"); !ok {
		t.Fatal("fresh failure entry was not cached")
	}
	if _, err := (cacheEntry{preview: Preview{Title: "cached"}}).result(); err != nil {
		t.Fatal("success result missing")
	}
	if ttl := entryTTL(failureNone); ttl != successTTL {
		t.Fatalf("success TTL = %v", ttl)
	}
	now = now.Add(failureTTL + time.Nanosecond)
	for i := 0; i <= maxCacheEntries; i++ {
		svc.store(fmt.Sprintf("key-%03d", i), cacheEntry{preview: Preview{URL: fmt.Sprint(i)}, fetchedAt: now.Add(time.Duration(i) * time.Second)})
	}
	if len(svc.entries) != maxCacheEntries {
		t.Fatalf("cache size = %d", len(svc.entries))
	}
	if _, ok := svc.entries["key-000"]; ok {
		t.Fatal("oldest entry was not evicted when cache was full")
	}
	if _, ok := svc.entries["failure"]; ok {
		t.Fatal("expired failure was not lazily evicted")
	}
}
