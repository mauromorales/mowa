package main

import (
	"path/filepath"
	"testing"
)

// TestLoadConfigMissingFileFallsBackToDefaults ensures a config path that does
// not exist yields the default config rather than an error, so the launchd
// service (which is pointed at a possibly-absent config.yaml) still starts.
func TestLoadConfigMissingFileFallsBackToDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	cfg, err := loadConfig(missing)
	if err != nil {
		t.Fatalf("expected defaults for a missing file, got error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected a config, got nil")
	}
	if cfg.Storage.Dir != "./storage" {
		t.Errorf("expected default storage dir, got %q", cfg.Storage.Dir)
	}
	if cfg.Messages.TimeoutSeconds != defaultSendTimeoutSeconds {
		t.Errorf("expected default timeout %d, got %d", defaultSendTimeoutSeconds, cfg.Messages.TimeoutSeconds)
	}
	if cfg.Messages.Groups == nil {
		t.Error("expected an initialized (non-nil) groups map")
	}
}

// TestLoadConfigUnreadableFileErrors ensures a real read error (not "missing")
// is still surfaced instead of being masked as a fallback to defaults.
func TestLoadConfigUnreadableFileErrors(t *testing.T) {
	// A directory can't be read as a file, producing a non-NotExist error.
	dir := t.TempDir()
	if _, err := loadConfig(dir); err == nil {
		t.Fatal("expected an error reading a directory as a config file, got nil")
	}
}
