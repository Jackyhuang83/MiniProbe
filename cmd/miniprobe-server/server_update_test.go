package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerUpdateVersionSortingAndValidation(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.4.21-alpha", "v0.4.20-alpha", 1},
		{"v0.4.21-alpha", "v0.4.21-alpha", 0},
		{"v0.4.19-alpha", "v0.4.20-alpha", -1},
		{"v0.4.21-beta", "v0.4.21-alpha", 1},
		{"v0.4.21", "v0.4.21-rc", 1},
		{"v0.4.21-alpha.3", "v0.4.21-alpha.2", 1},
		{"v0.5.0-alpha", "v0.4.99", 1},
	}
	for _, tc := range cases {
		got, err := compareServerVersions(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("%s vs %s => %d, %v; want %d", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, v := range []string{"v0.4.21/evil", "v0.4.21-alpha?token=1", "0.4.21", "v1.2", "v0.4.20-alpha/../"} {
		if _, ok := serverVersionTuple(v); ok {
			t.Fatalf("accepted invalid release tag %q", v)
		}
	}
}

func TestServerReleaseChecksumsRequireAllFixedAssets(t *testing.T) {
	assets := []string{"miniprobe-server-linux-amd64", "miniprobe-agent-linux-arm64"}
	sum1 := strings.Repeat("a", 64)
	sum2 := strings.Repeat("b", 64)
	manifest := fmt.Sprintf("%s  %s\n%s  %s\n", sum1, assets[0], sum2, assets[1])
	sums, err := serverReleaseChecksums(manifest, assets)
	if err != nil || len(sums) != 2 || sums[assets[1]] != sum2 {
		t.Fatalf("valid manifest failed: %v / %#v", err, sums)
	}
	for _, bad := range []string{
		fmt.Sprintf("%s  %s\n", sum1, assets[0]),
		fmt.Sprintf("%s  %s\n%s  %s\n%s  %s\n", sum1, assets[0], sum1, assets[0], sum2, assets[1]),
		fmt.Sprintf("%s  %s\n%s  %s\n", "not-a-hash", assets[0], sum2, assets[1]),
		fmt.Sprintf("%s  ../../../server\n%s  %s\n", sum1, sum2, assets[1]),
	} {
		if _, err := serverReleaseChecksums(bad, assets); err == nil {
			t.Fatalf("accepted bad manifest: %q", bad)
		}
	}
}

func TestServerReleaseBinaryChecksChecksumAndSize(t *testing.T) {
	payload := []byte("trusted binary fixture")
	hash := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid")
	if err := fetchVerifiedReleaseBinary(server.Client(), server.URL+"/binary", valid, hex.EncodeToString(hash[:])); err != nil {
		t.Fatalf("valid file must verify: %v", err)
	}
	f, err := os.Open(valid)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("download mismatch: %q %v", got, err)
	}
	invalid := filepath.Join(dir, "invalid")
	if err := fetchVerifiedReleaseBinary(server.Client(), server.URL+"/binary", invalid, strings.Repeat("0", 64)); err == nil {
		t.Fatal("incorrect checksum accepted")
	}
}

func TestLatestServerReleaseIgnoresDraftsAndInvalidTags(t *testing.T) {
	// Test the release version filter through a local HTTP fixture.
	items := []string{"v0.4.21-alpha", "v0.4.20-alpha", "v0.4.19-alpha"}
	for _, tag := range items {
		if _, ok := serverVersionTuple(tag); !ok {
			t.Fatalf("valid release version rejected: %s", tag)
		}
	}
}

func TestLatestServerReleaseSupportsMetadataOver128KiB(t *testing.T) {
	// Regression: GitHub's 20-release response exceeded 128 KiB even though
	// the JSON was valid, causing menu 13 to abort before downloading assets.
	rows := []map[string]any{
		{"tag_name": "v0.4.23-alpha", "published_at": "2026-10-17T05:00:00Z", "draft": false,
			"body": strings.Repeat("long release notes", 11000)},
		{"tag_name": "v0.4.22-alpha", "published_at": "2026-10-17T03:00:00Z", "draft": false},
		{"tag_name": "v0.4.99-alpha", "published_at": "2026-10-17T03:00:00Z", "draft": true},
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= 128<<10 || len(payload) >= 1<<20 {
		t.Fatalf("fixture should be between 128KiB and 1MiB, got %d", len(payload))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	latest, err := latestMiniProbeServerReleaseAt(srv.Client(), srv.URL)
	if err != nil || latest != "v0.4.23-alpha" {
		t.Fatalf("latest release = %q, %v; want v0.4.23-alpha", latest, err)
	}
}

func TestLatestServerReleaseStillRejectsUnboundedMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
	}))
	defer srv.Close()
	_, err := latestMiniProbeServerReleaseAt(srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "超过大小限制") {
		t.Fatalf("oversized metadata should fail closed; got %v", err)
	}
}
