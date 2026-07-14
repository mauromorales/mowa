package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := "/Users/test"
	cases := []struct {
		in   string
		want string
	}{
		{"~", home},
		{"~/Library/Logs/mowa.out", home + "/Library/Logs/mowa.out"},
		{"/usr/local/bin/mowa", "/usr/local/bin/mowa"},
		{"~notatilde/foo", "~notatilde/foo"}, // only a leading "~" or "~/" expands
		{"relative/path", "relative/path"},
	}
	for _, tc := range cases {
		if got := expandHome(tc.in, home); got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveServicePath(t *testing.T) {
	home := "/Users/test"

	// Empty value falls back to the default (with the default's ~ expanded).
	if got := resolveServicePath("  ", "~/Library/Logs/mowa.out", home); got != home+"/Library/Logs/mowa.out" {
		t.Errorf("empty value did not fall back to expanded default: %q", got)
	}

	// A provided value overrides the default and has its ~ expanded.
	if got := resolveServicePath("~/custom.yaml", "/unused", home); got != home+"/custom.yaml" {
		t.Errorf("provided value not expanded: %q", got)
	}

	// An absolute provided value is used verbatim.
	if got := resolveServicePath("/opt/mowa", "/unused", home); got != "/opt/mowa" {
		t.Errorf("absolute value changed: %q", got)
	}
}

func TestRenderLaunchdPlist(t *testing.T) {
	plist := renderLaunchdPlist("/usr/local/bin/mowa", "/Users/test/config.yaml", "/Users/test/out.log", "/Users/test/err.log")

	for _, want := range []string{
		"<key>Label</key>",
		"<string>" + launchdLabel + "</string>",
		"<string>/usr/local/bin/mowa</string>",
		"<string>-config</string>",
		"<string>/Users/test/config.yaml</string>",
		"<key>WorkingDirectory</key>",
		"<string>/Users/test</string>", // dir of the config path
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
		"<string>/Users/test/out.log</string>",
		"<string>/Users/test/err.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q\n%s", want, plist)
		}
	}
}

func TestEnsureExecutable(t *testing.T) {
	dir := t.TempDir()

	// A regular file with an executable bit passes.
	exe := filepath.Join(dir, "mowa")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureExecutable(exe); err != nil {
		t.Errorf("expected executable file to pass, got: %v", err)
	}

	// A non-existent path fails.
	if err := ensureExecutable(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Error("expected missing binary to fail")
	}

	// A directory fails.
	if err := ensureExecutable(dir); err == nil {
		t.Error("expected a directory to fail")
	}

	// A non-executable regular file fails.
	plain := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureExecutable(plain); err == nil {
		t.Error("expected non-executable file to fail")
	}
}

func TestRenderLaunchdPlistEscapesPaths(t *testing.T) {
	// A path with XML metacharacters must not break the document.
	plist := renderLaunchdPlist("/opt/a&b/mowa", "/c<d>/config.yaml", "/out.log", "/err.log")
	if strings.Contains(plist, "a&b") || strings.Contains(plist, "c<d>") {
		t.Errorf("plist did not escape XML metacharacters:\n%s", plist)
	}
	if !strings.Contains(plist, "a&amp;b") || !strings.Contains(plist, "c&lt;d&gt;") {
		t.Errorf("expected escaped metacharacters in plist:\n%s", plist)
	}
}
