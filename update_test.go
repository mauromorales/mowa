package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v0.4.2": "0.4.2",
		"0.4.2":  "0.4.2",
		"":       "",
		"v":      "",
		// only a single leading "v" is stripped
		"vv1.0": "v1.0",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssetNameForArch(t *testing.T) {
	if _, err := assetNameForArch("linux", "amd64"); err == nil {
		t.Error("expected error for non-darwin OS, got nil")
	}
	if _, err := assetNameForArch("darwin", "386"); err == nil {
		t.Error("expected error for unsupported arch, got nil")
	}

	arm, err := assetNameForArch("darwin", "arm64")
	if err != nil {
		t.Fatalf("darwin/arm64: unexpected error: %v", err)
	}
	if arm != "mowa_Darwin_arm64.zip" {
		t.Errorf("darwin/arm64 = %q, want mowa_Darwin_arm64.zip", arm)
	}

	amd, err := assetNameForArch("darwin", "amd64")
	if err != nil {
		t.Fatalf("darwin/amd64: unexpected error: %v", err)
	}
	if amd != "mowa_Darwin_x86_64.zip" {
		t.Errorf("darwin/amd64 = %q, want mowa_Darwin_x86_64.zip", amd)
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("some release bytes")
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	checksums := "deadbeef  other.zip\n" + hexSum + "  mowa_Darwin_arm64.zip\n"

	if err := verifyChecksum(data, checksums, "mowa_Darwin_arm64.zip"); err != nil {
		t.Errorf("expected checksum to verify, got: %v", err)
	}

	// Uppercase checksum digests should still match.
	if err := verifyChecksum(data, strings.ToUpper(hexSum)+"  mowa_Darwin_arm64.zip", "mowa_Darwin_arm64.zip"); err != nil {
		t.Errorf("expected uppercase checksum to verify, got: %v", err)
	}

	if err := verifyChecksum([]byte("tampered"), checksums, "mowa_Darwin_arm64.zip"); err == nil {
		t.Error("expected checksum mismatch error, got nil")
	}

	if err := verifyChecksum(data, checksums, "missing.zip"); err == nil {
		t.Error("expected error when asset absent from checksums, got nil")
	}
}

func TestEnsureAllowedHost(t *testing.T) {
	if err := ensureAllowedHost("https://api.github.com/repos/x", githubAPIHost); err != nil {
		t.Errorf("expected api.github.com to be allowed, got: %v", err)
	}
	if err := ensureAllowedHost("http://api.github.com/repos/x", githubAPIHost); err == nil {
		t.Error("expected non-HTTPS to be refused, got nil")
	}
	if err := ensureAllowedHost("https://evil.com/repos/x", githubAPIHost); err == nil {
		t.Error("expected unexpected host to be refused, got nil")
	}
}

func TestEnsureGitHubAssetHost(t *testing.T) {
	allowed := []string{
		"https://github.com/mauromorales/mowa/releases/download/v0.4.2/mowa_Darwin_arm64.zip",
		"https://objects.githubusercontent.com/foo",
		"https://release-assets.githubusercontent.com/bar",
	}
	for _, u := range allowed {
		if err := ensureGitHubAssetHost(u); err != nil {
			t.Errorf("expected %q to be allowed, got: %v", u, err)
		}
	}

	refused := []string{
		"http://github.com/x",                      // not HTTPS
		"https://evil.com/x",                       // wrong host
		"https://githubusercontent.com.evil.com/x", // suffix spoofing
		"https://notgithubusercontent.com/x",       // no dot separator
	}
	for _, u := range refused {
		if err := ensureGitHubAssetHost(u); err == nil {
			t.Errorf("expected %q to be refused, got nil", u)
		}
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	// Build an in-memory zip containing the mowa binary alongside other files.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	want := []byte("#!/bin/echo fake-mowa-binary")
	for name, content := range map[string][]byte{
		"README.md": []byte("readme"),
		"mowa":      want,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	got, err := extractBinaryFromZip(buf.Bytes())
	if err != nil {
		t.Fatalf("extractBinaryFromZip: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted binary = %q, want %q", got, want)
	}

	// A zip without a mowa binary should error.
	var empty bytes.Buffer
	ezw := zip.NewWriter(&empty)
	if _, err := ezw.Create("notmowa"); err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if err := ezw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if _, err := extractBinaryFromZip(empty.Bytes()); err == nil {
		t.Error("expected error when zip has no mowa binary, got nil")
	}
}
