package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandHome(t *testing.T) {
	// Build home and expectations with filepath so the test is OS-agnostic:
	// expandHome joins with filepath.Join, so a hardcoded "/"-joined expectation
	// would fail on non-Unix builders.
	home := filepath.FromSlash("/Users/test")
	cases := []struct {
		in   string
		want string
	}{
		{"~", home},
		{"~/Library/Logs/mowa.out", filepath.Join(home, "Library", "Logs", "mowa.out")},
		{"/usr/local/bin/mowa", "/usr/local/bin/mowa"}, // no leading "~", returned verbatim
		{"~notatilde/foo", "~notatilde/foo"},           // only a leading "~" or "~/" expands
		{"relative/path", "relative/path"},
	}
	for _, tc := range cases {
		if got := expandHome(tc.in, home); got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveServicePath(t *testing.T) {
	home := filepath.FromSlash("/Users/test")

	// Empty value falls back to the default (with the default's ~ expanded).
	if got := resolveServicePath("  ", "~/Library/Logs/mowa.out", home); got != filepath.Join(home, "Library", "Logs", "mowa.out") {
		t.Errorf("empty value did not fall back to expanded default: %q", got)
	}

	// A provided value overrides the default and has its ~ expanded.
	if got := resolveServicePath("~/custom.yaml", "/unused", home); got != filepath.Join(home, "custom.yaml") {
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

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "com.example.test.plist")

	if err := writeFileAtomic(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}

	// Overwriting an existing file works and leaves no stray temp files behind.
	if err := writeFileAtomic(path, []byte("world"), 0o600); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the plist to remain, found: %v", names)
	}
}

func TestInstallFlagParsing(t *testing.T) {
	// `-h` is a clean exit (ContinueOnError + flag.ErrHelp), not an install
	// failure, and it must return before any launchctl/filesystem work.
	if err := runInstall([]string{"-h"}); err != nil {
		t.Errorf("runInstall(-h) = %v, want nil", err)
	}

	// An unknown flag surfaces as a returned error rather than os.Exit.
	if err := runInstall([]string{"--definitely-not-a-flag"}); err == nil {
		t.Error("runInstall(--definitely-not-a-flag) = nil, want an error")
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
