package main

import (
	"os"
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

// TestLoadConfigSoftwareUpdateCheck ensures the software_update_check section
// is parsed with the messaging-style notify list, and that omitted fields fall
// back to defaults.
func TestLoadConfigSoftwareUpdateCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
messages:
  groups:
    admin:
      - "+1234567890"
software_update_check:
  notify:
    - admin
    - "+4915112345678"
  schedule: "04:30"
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.SoftwareUpdateCheck.isEnabled() {
		t.Error("expected update check to be enabled when notify has recipients")
	}
	if len(cfg.SoftwareUpdateCheck.Notify) != 2 || cfg.SoftwareUpdateCheck.Notify[0] != "admin" {
		t.Errorf("notify = %v", cfg.SoftwareUpdateCheck.Notify)
	}
	if cfg.SoftwareUpdateCheck.Schedule != "04:30" {
		t.Errorf("schedule = %q, want 04:30", cfg.SoftwareUpdateCheck.Schedule)
	}
	if cfg.SoftwareUpdateCheck.TimeoutSeconds != defaultUpdateCheckTimeoutSeconds {
		t.Errorf("timeout = %d, want default %d", cfg.SoftwareUpdateCheck.TimeoutSeconds, defaultUpdateCheckTimeoutSeconds)
	}

	// Defaults when the section is absent entirely: disabled, default schedule.
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	def, err := loadConfig(missing)
	if err != nil {
		t.Fatal(err)
	}
	if def.SoftwareUpdateCheck.isEnabled() {
		t.Error("update check must be disabled by default")
	}
	if def.SoftwareUpdateCheck.Schedule != defaultUpdateCheckSchedule {
		t.Errorf("default schedule = %q, want %q", def.SoftwareUpdateCheck.Schedule, defaultUpdateCheckSchedule)
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
