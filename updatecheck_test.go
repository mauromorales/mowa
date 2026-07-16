package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realSoftwareUpdateOutput is verbatim `softwareupdate --list` output captured
// on macOS 26 (Tahoe): two non-restart Command Line Tools updates and one
// restart-required OS update.
const realSoftwareUpdateOutput = `Software Update Tool

Finding available software
Software Update found the following new or updated software:
* Label: Command Line Tools for Xcode 26.5-26.5
	Title: Command Line Tools for Xcode 26.5, Version: 26.5, Size: 920416KiB, Recommended: YES,
* Label: Command Line Tools for Xcode 26.6-26.6
	Title: Command Line Tools for Xcode 26.6, Version: 26.6, Size: 920431KiB, Recommended: YES,
* Label: macOS Tahoe 26.5.2-25F84
	Title: macOS Tahoe 26.5.2, Version: 26.5.2, Size: 3709215KiB, Recommended: YES, Action: restart,
`

func TestParseSoftwareUpdateList(t *testing.T) {
	updates := parseSoftwareUpdateList(realSoftwareUpdateOutput)
	if len(updates) != 3 {
		t.Fatalf("parsed %d updates, want 3: %+v", len(updates), updates)
	}

	clt := updates[0]
	if clt.Label != "Command Line Tools for Xcode 26.5-26.5" {
		t.Errorf("label = %q", clt.Label)
	}
	if clt.Title != "Command Line Tools for Xcode 26.5" || clt.Version != "26.5" {
		t.Errorf("title/version = %q/%q", clt.Title, clt.Version)
	}
	if clt.RestartRequired {
		t.Error("Command Line Tools update must not be restart-required")
	}

	macos := updates[2]
	if macos.Label != "macOS Tahoe 26.5.2-25F84" {
		t.Errorf("label = %q", macos.Label)
	}
	if macos.Title != "macOS Tahoe 26.5.2" || macos.Version != "26.5.2" {
		t.Errorf("title/version = %q/%q", macos.Title, macos.Version)
	}
	if !macos.RestartRequired {
		t.Error("macOS update must be restart-required (Action: restart)")
	}
}

func TestParseSoftwareUpdateListNoUpdates(t *testing.T) {
	for name, output := range map[string]string{
		"no new software": "Software Update Tool\n\nFinding available software\nNo new software available.\n",
		"empty":           "",
		"garbage":         "could not connect to the update server\n",
	} {
		if updates := parseSoftwareUpdateList(output); len(updates) != 0 {
			t.Errorf("%s: parsed %d updates, want 0", name, len(updates))
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		u    availableUpdate
		want string
	}{
		// Title already contains the version: no duplication.
		{availableUpdate{Label: "macOS Tahoe 26.5.2-25F84", Title: "macOS Tahoe 26.5.2", Version: "26.5.2"}, "macOS Tahoe 26.5.2"},
		// Version missing from the title: appended.
		{availableUpdate{Label: "Safari-x", Title: "Safari", Version: "17.5"}, "Safari 17.5"},
		// No title: fall back to the label.
		{availableUpdate{Label: "SomeLabel-1.0", Version: "1.0"}, "SomeLabel-1.0"},
	}
	for _, tc := range cases {
		if got := tc.u.displayName(); got != tc.want {
			t.Errorf("displayName(%+v) = %q, want %q", tc.u, got, tc.want)
		}
	}
}

func TestUpdateNotificationMessage(t *testing.T) {
	one := updateNotificationMessage("macmini.local", []availableUpdate{
		{Title: "macOS Tahoe 26.5.2", Version: "26.5.2"},
	})
	// The user-specified format: leading ⬆️, host, and the update name.
	if !strings.HasPrefix(one, "⬆️ ") {
		t.Errorf("message must start with ⬆️: %q", one)
	}
	for _, want := range []string{"macmini.local", "macOS Tahoe 26.5.2", "install manually"} {
		if !strings.Contains(one, want) {
			t.Errorf("message missing %q: %q", want, one)
		}
	}

	two := updateNotificationMessage("h", []availableUpdate{
		{Title: "macOS Tahoe 26.5.2", Version: "26.5.2"},
		{Title: "macOS Tahoe 26.6", Version: "26.6"},
	})
	if !strings.Contains(two, "updates available") || !strings.Contains(two, "macOS Tahoe 26.5.2, macOS Tahoe 26.6") {
		t.Errorf("multi-update message wrong: %q", two)
	}
}

func TestUpdateCheckStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", updateCheckStateFileName)

	// A missing file is an empty state, not an error.
	if state := loadUpdateCheckState(path); len(state.NotifiedLabels) != 0 {
		t.Errorf("missing file: state = %+v, want empty", state)
	}

	want := updateCheckState{NotifiedLabels: []string{"macOS Tahoe 26.5.2-25F84"}}
	if err := saveUpdateCheckState(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadUpdateCheckState(path)
	if len(got.NotifiedLabels) != 1 || got.NotifiedLabels[0] != want.NotifiedLabels[0] {
		t.Errorf("state = %+v, want %+v", got, want)
	}

	// A corrupt file degrades to an empty state instead of failing the check.
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if state := loadUpdateCheckState(path); len(state.NotifiedLabels) != 0 {
		t.Errorf("corrupt file: state = %+v, want empty", state)
	}
}

func TestUpdateCheckStatePath(t *testing.T) {
	got, err := updateCheckStatePath("/Users/test/Library/Application Support/mowa/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/Users/test/Library/Application Support/mowa", updateCheckStateFileName)
	if got != want {
		t.Errorf("statePath = %q, want %q", got, want)
	}

	// Without a config path there is no well-defined state location.
	if _, err := updateCheckStatePath("  "); err == nil {
		t.Error("expected an error for an empty config path")
	}
}

func TestSoftwareUpdateCheckIsEnabled(t *testing.T) {
	off := false
	on := true
	cases := []struct {
		name string
		cfg  SoftwareUpdateCheckConfig
		want bool
	}{
		{"zero config", SoftwareUpdateCheckConfig{}, false},
		{"notify only", SoftwareUpdateCheckConfig{Notify: []string{"admin"}}, true},
		{"explicitly disabled", SoftwareUpdateCheckConfig{Notify: []string{"admin"}, Enabled: &off}, false},
		{"explicitly enabled", SoftwareUpdateCheckConfig{Notify: []string{"admin"}, Enabled: &on}, true},
		{"enabled but nobody to notify", SoftwareUpdateCheckConfig{Enabled: &on}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.isEnabled(); got != tc.want {
			t.Errorf("%s: isEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseSchedule(t *testing.T) {
	cases := []struct {
		in           string
		hour, minute int
	}{
		{"", 3, 0}, // empty falls back to the 03:00 default
		{"03:00", 3, 0},
		{"23:59", 23, 59},
		{"00:00", 0, 0},
	}
	for _, tc := range cases {
		hour, minute, err := parseSchedule(tc.in)
		if err != nil {
			t.Errorf("parseSchedule(%q): %v", tc.in, err)
			continue
		}
		if hour != tc.hour || minute != tc.minute {
			t.Errorf("parseSchedule(%q) = %d:%d, want %d:%d", tc.in, hour, minute, tc.hour, tc.minute)
		}
	}

	for _, bad := range []string{"24:00", "3pm", "03:60", "3:0:0", "nightly"} {
		if _, _, err := parseSchedule(bad); err == nil {
			t.Errorf("parseSchedule(%q) = nil error, want an error", bad)
		}
	}
}

func TestRenderUpdateCheckPlist(t *testing.T) {
	plist := renderUpdateCheckPlist("/usr/local/bin/mowa", "/Users/test/config.yaml", "/Users/test/out.log", "/Users/test/err.log", 3, 30)

	for _, want := range []string{
		"<string>" + updateCheckLabel + "</string>",
		"<string>/usr/local/bin/mowa</string>",
		"<string>check-updates</string>",
		"<string>-config</string>",
		"<string>/Users/test/config.yaml</string>",
		"<key>StartCalendarInterval</key>",
		"<integer>3</integer>",
		"<integer>30</integer>",
		"<string>/Users/test/out.log</string>",
		"<string>/Users/test/err.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q\n%s", want, plist)
		}
	}

	// A scheduled one-shot must not keep itself alive or run at load.
	if strings.Contains(plist, "KeepAlive") || strings.Contains(plist, "RunAtLoad") {
		t.Errorf("update-check plist must not contain KeepAlive/RunAtLoad:\n%s", plist)
	}
}

func TestCheckUpdatesFlagParsing(t *testing.T) {
	// `-h` is a clean exit, mirroring `mowa install -h`.
	if err := runCheckUpdates([]string{"-h"}); err != nil {
		t.Errorf("runCheckUpdates(-h) = %v, want nil", err)
	}
	if err := runCheckUpdates([]string{"--definitely-not-a-flag"}); err == nil {
		t.Error("runCheckUpdates(--definitely-not-a-flag) = nil, want an error")
	}
}

func TestCheckUpdatesUnconfiguredIsNoop(t *testing.T) {
	// With no config (defaults: no notify recipients) the subcommand must exit
	// cleanly without shelling out to softwareupdate or writing any state.
	prev := appConfig
	defer func() { appConfig = prev }()
	if err := runCheckUpdates(nil); err != nil {
		t.Errorf("runCheckUpdates with no config = %v, want nil", err)
	}
}
