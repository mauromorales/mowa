package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Software update check constants (issue #17). The check runs as the short-lived
// `mowa check-updates` subcommand, scheduled by a launchd StartCalendarInterval
// agent, and messages the configured recipients when a restart-required macOS
// update is pending so it can be installed manually.
const (
	// defaultUpdateCheckSchedule is the local time of day the check runs when
	// the config does not set software_update_check.schedule. Night-time by
	// design: quiet, and launchd re-fires a missed interval on wake.
	defaultUpdateCheckSchedule = "03:00"

	// defaultUpdateCheckTimeoutSeconds bounds `softwareupdate --list`, which
	// scans Apple's servers and routinely takes 30-60s; nothing is waiting on
	// the result, so the default is generous.
	defaultUpdateCheckTimeoutSeconds = 300

	// updateCheckStateFileName holds the labels already notified about, stored
	// next to the config file so the short-lived subcommand and the server
	// agree on its location.
	updateCheckStateFileName = "update-check-state.json"
)

// availableUpdate is one entry parsed from `softwareupdate --list`.
type availableUpdate struct {
	// Label uniquely identifies the update+version (e.g.
	// "macOS Tahoe 26.5.2-25F84") and is what dedupe state tracks.
	Label string
	// Title is the human-readable name (e.g. "macOS Tahoe 26.5.2").
	Title string
	// Version as reported by softwareupdate (e.g. "26.5.2").
	Version string
	// RestartRequired is true for updates listing "Action: restart" — the OS
	// updates that reboot into Setup Assistant and break iMessage, which are
	// the ones worth a notification.
	RestartRequired bool
}

// displayName is what the notification shows for an update: the title (which
// for macOS updates already includes the version), falling back to the label,
// with the version appended when it isn't already part of the name.
func (u availableUpdate) displayName() string {
	name := u.Title
	if name == "" {
		name = u.Label
	}
	if u.Version != "" && !strings.Contains(name, u.Version) {
		name += " " + u.Version
	}
	return name
}

// updateCheckState is the JSON persisted between runs to notify once per
// newly-available update.
type updateCheckState struct {
	// NotifiedLabels are the restart-required update labels recipients have
	// already been messaged about. Labels no longer listed by softwareupdate
	// (i.e. installed) are pruned each run, so a future re-release of the same
	// label would notify again.
	NotifiedLabels []string `json:"notified_labels"`
}

// runCheckUpdates implements `mowa check-updates`: list available software
// updates, and message the configured recipients about restart-required ones
// they have not been told about yet. When software_update_check is not
// configured (no notify recipients, or enabled: false) it exits silently, so
// the LaunchAgent can be installed unconditionally.
func runCheckUpdates(args []string) error {
	fs := flag.NewFlagSet("check-updates", flag.ContinueOnError)
	configFlag := fs.String("config", "", "Path to the config file (same flag the server takes)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mowa check-updates [flags]\n\nChecks `softwareupdate --list` for restart-required macOS updates and\nmessages the recipients in software_update_check.notify about new ones.\nDoes nothing unless software_update_check is configured.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	configPath := strings.TrimSpace(*configFlag)
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	// sendMessages/expandGroups read the package-level config, exactly like the
	// server does.
	appConfig = cfg

	if !cfg.SoftwareUpdateCheck.isEnabled() {
		log.Printf("software update check is not configured (software_update_check.notify is empty or enabled is false); nothing to do")
		return nil
	}

	timeout := time.Duration(cfg.SoftwareUpdateCheck.TimeoutSeconds) * time.Second
	output, err := listSoftwareUpdates(timeout)
	if err != nil {
		return fmt.Errorf("softwareupdate --list failed: %w", err)
	}

	var restartUpdates []availableUpdate
	for _, u := range parseSoftwareUpdateList(output) {
		if u.RestartRequired {
			restartUpdates = append(restartUpdates, u)
		}
	}

	statePath, err := updateCheckStatePath(configPath)
	if err != nil {
		return err
	}
	state := loadUpdateCheckState(statePath)

	notified := make(map[string]bool, len(state.NotifiedLabels))
	for _, label := range state.NotifiedLabels {
		notified[label] = true
	}

	var fresh []availableUpdate
	for _, u := range restartUpdates {
		if !notified[u.Label] {
			fresh = append(fresh, u)
		}
	}

	if len(restartUpdates) == 0 {
		log.Printf("no restart-required updates available; not notifying")
	} else if len(fresh) == 0 {
		log.Printf("all %d restart-required update(s) already notified; not notifying again", len(restartUpdates))
	}

	sendFailed := false
	if len(fresh) > 0 {
		host, err := os.Hostname()
		if err != nil {
			host = "this Mac"
		}
		message := updateNotificationMessage(host, fresh)
		log.Printf("notifying %v: %s", cfg.SoftwareUpdateCheck.Notify, message)

		anySuccess := false
		for _, result := range sendMessages(expandGroups(cfg.SoftwareUpdateCheck.Notify), message) {
			if result.Success {
				anySuccess = true
			} else if result.Error != nil {
				log.Printf("⚠️ failed to notify %s: %s", result.Recipient, *result.Error)
			}
		}
		// Only record the labels once someone actually received the message, so
		// a completely failed send is retried on the next scheduled run.
		if anySuccess {
			for _, u := range fresh {
				notified[u.Label] = true
			}
		} else {
			sendFailed = true
		}
	}

	// Persist the notified labels that are still pending; anything no longer
	// listed has been installed and is dropped.
	var stillPending []string
	for _, u := range restartUpdates {
		if notified[u.Label] {
			stillPending = append(stillPending, u.Label)
		}
	}
	sort.Strings(stillPending)
	if err := saveUpdateCheckState(statePath, updateCheckState{NotifiedLabels: stillPending}); err != nil {
		return fmt.Errorf("failed to save update-check state: %w", err)
	}

	if sendFailed {
		return fmt.Errorf("update notification could not be delivered to any recipient")
	}
	return nil
}

// listSoftwareUpdates runs `softwareupdate --list` under a bounded deadline and
// returns its combined output. The command hits Apple's update servers, so the
// deadline keeps a stuck scan from wedging the scheduled job.
func listSoftwareUpdates(timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "softwareupdate", "--list").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		return string(out), fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// parseSoftwareUpdateList parses the modern (macOS 12+) `softwareupdate --list`
// format:
//
//   - Label: macOS Tahoe 26.5.2-25F84
//     Title: macOS Tahoe 26.5.2, Version: 26.5.2, Size: 3709215KiB, Recommended: YES, Action: restart,
//
// "No new software available." (and any unrecognized output) yields no updates.
func parseSoftwareUpdateList(output string) []availableUpdate {
	var updates []availableUpdate
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "* Label:") {
			continue
		}
		u := availableUpdate{Label: strings.TrimSpace(strings.TrimPrefix(trimmed, "* Label:"))}
		if i+1 < len(lines) {
			if detail := strings.TrimSpace(lines[i+1]); strings.HasPrefix(detail, "Title:") {
				u.Title, u.Version, u.RestartRequired = parseUpdateDetail(detail)
			}
		}
		updates = append(updates, u)
	}
	return updates
}

// parseUpdateDetail parses the comma-separated "Key: value" detail line under a
// label. Apple's own titles ("macOS Tahoe 26.5.2", "Command Line Tools for
// Xcode 26.6") contain no commas; a hypothetical comma in a title would only
// truncate that title, never misparse the restart flag.
func parseUpdateDetail(detail string) (title, version string, restart bool) {
	for _, part := range strings.Split(detail, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "Title:"):
			title = strings.TrimSpace(strings.TrimPrefix(part, "Title:"))
		case strings.HasPrefix(part, "Version:"):
			version = strings.TrimSpace(strings.TrimPrefix(part, "Version:"))
		case strings.HasPrefix(part, "Action:"):
			restart = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(part, "Action:")), "restart")
		}
	}
	return title, version, restart
}

// updateNotificationMessage builds the iMessage text for newly-available
// restart-required updates. The leading ⬆️ identifies upgrade notifications
// at a glance (issue #17).
func updateNotificationMessage(host string, updates []availableUpdate) string {
	names := make([]string, 0, len(updates))
	for _, u := range updates {
		names = append(names, u.displayName())
	}
	noun := "update"
	if len(updates) > 1 {
		noun = "updates"
	}
	return fmt.Sprintf("⬆️ macOS %s available on %s — install manually: %s", noun, host, strings.Join(names, ", "))
}

// updateCheckStatePath places the dedupe state next to the config file, which
// is the one location both the scheduled subcommand and the server can derive
// identically. The check is only enabled through a config file, so configPath
// is always non-empty here; the empty case is a defensive error.
func updateCheckStatePath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) == "" {
		return "", fmt.Errorf("cannot locate update-check state without a config path")
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("could not resolve config path %q: %w", configPath, err)
	}
	return filepath.Join(filepath.Dir(abs), updateCheckStateFileName), nil
}

// loadUpdateCheckState reads the persisted state, treating a missing or
// unreadable file as "nothing notified yet" — the worst case is one repeated
// notification, which beats failing the whole check.
func loadUpdateCheckState(path string) updateCheckState {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("⚠️ could not read update-check state %s (%v); assuming nothing notified", path, err)
		}
		return updateCheckState{}
	}
	var state updateCheckState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("⚠️ could not parse update-check state %s (%v); assuming nothing notified", path, err)
		return updateCheckState{}
	}
	return state
}

// saveUpdateCheckState persists the state atomically so a crash mid-write can't
// corrupt it into re-notification loops.
func saveUpdateCheckState(path string, state updateCheckState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0600)
}
