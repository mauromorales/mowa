package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// launchd service constants. The label is fixed so the plist, the launchctl
// service target, and the reload logic all agree on the same identity.
const (
	launchdLabel = "com.mauromorales.mowa"

	// updateCheckLabel is the second, calendar-scheduled agent that runs
	// `mowa check-updates` (issue #17). Installed by `mowa install` and kept in
	// sync by the server at startup, so upgrades pick it up without root.
	updateCheckLabel = "com.mauromorales.mowa-update-check"

	// defaultBinaryPath is the fallback binary location used only when the
	// running executable's path can't be resolved. Normally `mowa install`
	// points the service at the very binary the user invoked.
	defaultBinaryPath = "/usr/local/bin/mowa"

	// launchctlTimeout bounds each launchctl invocation so a wedged launchd
	// can't hang the install command indefinitely.
	launchctlTimeout = 10 * time.Second
)

// pidLineRegexp extracts the running pid from `launchctl print` output, whose
// service dump contains a line like "\tpid = 1234".
var pidLineRegexp = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)

// runInstall implements `mowa install`: it generates the launchd agent plist,
// writes it to ~/Library/LaunchAgents/com.mauromorales.mowa.plist, and loads +
// starts the service so mowa runs at login and stays alive (KeepAlive).
//
// It is idempotent: re-running unloads any existing instance of the label and
// bootstraps the freshly written plist. Because this is a short-lived command
// separate from the long-running server, reloading a running server is safe —
// launchd relaunches it from the new plist.
func runInstall(args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	binaryFlag := fs.String("binary", "", "Path to the mowa binary launchd should run (default: the running executable)")
	configFlag := fs.String("config", "", "Path to the config file passed via -config (default: ~/Library/Application Support/mowa/config.yaml)")
	stdoutFlag := fs.String("stdout", "", "Path for the service's stdout log (default: ~/Library/Logs/mowa.out)")
	stderrFlag := fs.String("stderr", "", "Path for the service's stderr log (default: ~/Library/Logs/mowa.err)")
	portFlag := fs.String("port", "", "Port for the service via MOWA_PORT (default: the MOWA_PORT env var at install time, else mowa's built-in 8080)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mowa install [flags]\n\nInstalls mowa as a launchd login service. All flags are optional.\nA leading \"~\" in any path is expanded to your home directory.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		// `mowa install -h` is a clean exit, not an install failure; the flag
		// package has already printed the usage text.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	// Fail fast if launchd tooling is absent (non-macOS host or a minimal PATH)
	// before we write any plist or create any directories, so a failed install
	// leaves nothing behind.
	if _, err := exec.LookPath("launchctl"); err != nil {
		return fmt.Errorf("launchctl not found on PATH; `mowa install` requires macOS launchd: %w", err)
	}

	// Resolve the port the service should listen on. An explicit --port wins;
	// otherwise capture MOWA_PORT from the current environment at install time so
	// the service matches how the user runs mowa interactively. When neither is
	// set, the plist omits MOWA_PORT and mowa uses its built-in default (8080).
	port := strings.TrimSpace(*portFlag)
	if port == "" {
		port = strings.TrimSpace(os.Getenv("MOWA_PORT"))
	}
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid port %q: must be a number between 1 and 65535", port)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not resolve the current user's home directory: %w", err)
	}

	binaryPath := resolveServicePath(*binaryFlag, defaultBinaryLocation(), home)
	configPath := resolveServicePath(*configFlag, filepath.Join(home, "Library", "Application Support", "mowa", "config.yaml"), home)
	stdoutLog := resolveServicePath(*stdoutFlag, filepath.Join(home, "Library", "Logs", "mowa.out"), home)
	stderrLog := resolveServicePath(*stderrFlag, filepath.Join(home, "Library", "Logs", "mowa.err"), home)

	// launchd expects absolute paths in ProgramArguments and for its log/config
	// paths. A "~" has already been expanded; anchor anything still relative
	// (e.g. a relative os.Executable() or a relative flag value) to the current
	// working directory so the plist is unambiguous once launchd runs it.
	for _, p := range []*string{&binaryPath, &configPath, &stdoutLog, &stderrLog} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return fmt.Errorf("could not resolve %q to an absolute path: %w", *p, err)
		}
		*p = abs
	}

	// The binary must exist and be executable now; otherwise the install
	// "succeeds" but launchd fails to start the service later with an opaque
	// error.
	if err := ensureExecutable(binaryPath); err != nil {
		return err
	}

	// Ensure the directories launchd and the config file need exist. The config
	// file itself is left absent on purpose: loadConfig falls back to defaults
	// when it is missing, so we must not create or overwrite it here. These live
	// under the user's home, so create them privately (0700).
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	for _, dir := range []string{launchAgentsDir, filepath.Dir(configPath), filepath.Dir(stdoutLog), filepath.Dir(stderrLog)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	plistPath := filepath.Join(launchAgentsDir, launchdLabel+".plist")
	if err := writeFileAtomic(plistPath, []byte(renderLaunchdPlist(binaryPath, configPath, stdoutLog, stderrLog, port)), 0600); err != nil {
		return fmt.Errorf("failed to write launchd plist %s: %w", plistPath, err)
	}
	fmt.Printf("Wrote launchd plist to %s\n", plistPath)

	uid := os.Getuid()
	domainTarget := fmt.Sprintf("gui/%d", uid)
	serviceTarget := fmt.Sprintf("gui/%d/%s", uid, launchdLabel)

	// Unload any existing instance of the label so bootstrapping a refreshed
	// plist doesn't fail with "service already bootstrapped".
	if _, loaded := servicePID(serviceTarget); loaded {
		if out, err := runLaunchctl("bootout", serviceTarget); err != nil {
			fmt.Printf("note: %v\n", err)
			if out != "" {
				fmt.Println(indent(out))
			}
		} else {
			fmt.Printf("Unloaded the existing service (%s)\n", serviceTarget)
		}
	}

	if out, err := runLaunchctl("bootstrap", domainTarget, plistPath); err != nil {
		if out != "" {
			return fmt.Errorf("%w\n%s", err, indent(out))
		}
		return err
	}

	fmt.Printf("✅ Installed %s\n", launchdLabel)
	fmt.Printf("   binary: %s\n", binaryPath)
	fmt.Printf("   config: %s\n", configPath)
	if port != "" {
		fmt.Printf("   port:   %s (MOWA_PORT)\n", port)
	} else {
		fmt.Println("   port:   8080 (default; pass --port or set MOWA_PORT to change)")
	}
	fmt.Println("   The service will start automatically at login and stay alive (KeepAlive).")

	// `bootstrap` only means the job was loaded, not that it stayed running — a
	// job that crashes immediately (e.g. a port conflict) still bootstraps fine.
	// Give launchd a moment to spawn the process, then report what actually
	// happened so "Installed" isn't misread as "healthy".
	pid, loaded := waitForServiceStart(serviceTarget, 2*time.Second)
	fmt.Print(serviceStatusReport(pid, loaded))
	fmt.Printf("   Inspect it with: launchctl print %s\n", serviceTarget)

	// Also install the update-check agent. It is always installed; whether it
	// actually notifies is governed by software_update_check in the config, and
	// `mowa check-updates` exits silently when that is absent. The schedule is
	// read from the config the service will use.
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("could not read config to schedule the update check: %w", err)
	}
	changed, err := ensureUpdateCheckAgent(binaryPath, configPath, cfg.SoftwareUpdateCheck.Schedule, home)
	if err != nil {
		return fmt.Errorf("failed to install the update-check agent: %w", err)
	}
	if changed {
		fmt.Printf("✅ Installed %s (runs `mowa check-updates` daily at %s)\n", updateCheckLabel, scheduleOrDefault(cfg.SoftwareUpdateCheck.Schedule))
	} else {
		fmt.Printf("✅ %s already up to date (runs `mowa check-updates` daily at %s)\n", updateCheckLabel, scheduleOrDefault(cfg.SoftwareUpdateCheck.Schedule))
	}
	if !cfg.SoftwareUpdateCheck.isEnabled() {
		fmt.Println("   Note: update notifications are off until software_update_check.notify is set in the config.")
	}
	return nil
}

// scheduleOrDefault normalizes an empty schedule to the built-in default for
// display and plist rendering.
func scheduleOrDefault(schedule string) string {
	if strings.TrimSpace(schedule) == "" {
		return defaultUpdateCheckSchedule
	}
	return strings.TrimSpace(schedule)
}

// parseSchedule parses a "HH:MM" (24h) time of day.
func parseSchedule(schedule string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", scheduleOrDefault(schedule))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid software_update_check.schedule %q: want HH:MM (24h), e.g. \"03:00\"", schedule)
	}
	return t.Hour(), t.Minute(), nil
}

// ensureUpdateCheckAgent writes and bootstraps the update-check LaunchAgent,
// used both by `mowa install` and by the server at startup (so an existing
// installation gains or re-syncs the agent after an upgrade or a schedule
// change — everything here stays in the per-user gui domain, no root needed).
// It is idempotent: when the plist already matches and the job is loaded it
// does nothing, so a server restart causes no launchd churn. Returns whether
// anything was (re)installed.
func ensureUpdateCheckAgent(binaryPath, configPath, schedule, home string) (changed bool, err error) {
	hour, minute, err := parseSchedule(schedule)
	if err != nil {
		return false, err
	}

	// The agent logs next to nothing, but a scan failure should be findable.
	stdoutLog := filepath.Join(home, "Library", "Logs", "mowa-update-check.out")
	stderrLog := filepath.Join(home, "Library", "Logs", "mowa-update-check.err")

	plist := renderUpdateCheckPlist(binaryPath, configPath, stdoutLog, stderrLog, hour, minute)
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(launchAgentsDir, updateCheckLabel+".plist")
	serviceTarget := fmt.Sprintf("gui/%d/%s", os.Getuid(), updateCheckLabel)

	existing, readErr := os.ReadFile(plistPath)
	_, loaded := servicePID(serviceTarget)
	if readErr == nil && string(existing) == plist && loaded {
		return false, nil
	}

	for _, dir := range []string{launchAgentsDir, filepath.Dir(stdoutLog)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return false, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	if err := writeFileAtomic(plistPath, []byte(plist), 0600); err != nil {
		return false, fmt.Errorf("failed to write launchd plist %s: %w", plistPath, err)
	}

	if loaded {
		if out, err := runLaunchctl("bootout", serviceTarget); err != nil {
			// Non-fatal: bootstrap below reports the real problem if there is one.
			log.Printf("note: %v\n%s", err, indent(out))
		}
	}
	if out, err := runLaunchctl("bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plistPath); err != nil {
		if out != "" {
			return false, fmt.Errorf("%w\n%s", err, indent(out))
		}
		return false, err
	}
	return true, nil
}

// ensureUpdateCheckAgentAtStartup is the server-startup self-heal: when the
// loaded config enables update checks, make sure the scheduled agent exists and
// matches the config. Runs asynchronously and only logs on failure — a broken
// launchd interaction must never take the HTTP server down with it.
func ensureUpdateCheckAgentAtStartup(configPath string) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		log.Printf("⚠️ software_update_check is enabled but launchctl is unavailable; cannot schedule the update check: %v", err)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("⚠️ could not resolve home directory to schedule the update check: %v", err)
		return
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		log.Printf("⚠️ could not resolve config path to schedule the update check: %v", err)
		return
	}
	changed, err := ensureUpdateCheckAgent(defaultBinaryLocation(), absConfig, appConfig.SoftwareUpdateCheck.Schedule, home)
	if err != nil {
		log.Printf("⚠️ could not install the update-check agent: %v", err)
		return
	}
	if changed {
		log.Printf("✅ Installed %s (runs `mowa check-updates` daily at %s)", updateCheckLabel, scheduleOrDefault(appConfig.SoftwareUpdateCheck.Schedule))
	}
}

// renderUpdateCheckPlist builds the calendar-scheduled agent that runs
// `mowa check-updates` at the given local time. Unlike the server agent it has
// no RunAtLoad/KeepAlive: launchd starts it at the scheduled time (or on the
// next wake if the Mac was asleep) and it exits when done. WorkingDirectory
// matches the server agent so relative paths resolve identically.
func renderUpdateCheckPlist(binaryPath, configPath, stdoutLog, stderrLog string, hour, minute int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>check-updates</string>
        <string>-config</string>
        <string>%s</string>
    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>%d</integer>
        <key>Minute</key>
        <integer>%d</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>
`,
		html.EscapeString(updateCheckLabel),
		html.EscapeString(binaryPath),
		html.EscapeString(configPath),
		html.EscapeString(filepath.Dir(configPath)),
		hour,
		minute,
		html.EscapeString(stdoutLog),
		html.EscapeString(stderrLog),
	)
}

// waitForServiceStart polls the service target until it reports a running pid or
// the timeout elapses, returning the pid (0 if none) and whether the job is
// loaded. launchd spawns the process asynchronously after bootstrap, so a brief
// poll avoids a false "not running" report for a service that is simply still
// starting.
func waitForServiceStart(serviceTarget string, timeout time.Duration) (pid int, loaded bool) {
	deadline := time.Now().Add(timeout)
	for {
		pid, loaded = servicePID(serviceTarget)
		if pid > 0 || time.Now().After(deadline) {
			return pid, loaded
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// serviceStatusReport renders a one-line status from a service's pid/loaded
// state after bootstrap: running with a pid, loaded but not (yet) running
// (likely a startup crash — worth checking logs and port conflicts), or not
// loaded at all.
func serviceStatusReport(pid int, loaded bool) string {
	switch {
	case pid > 0:
		return fmt.Sprintf("   status: running (pid %d)\n", pid)
	case loaded:
		return "   ⚠️  status: loaded but not running — it likely crashed on startup; check the logs above/below and for a port conflict\n"
	default:
		return "   ⚠️  status: not loaded after bootstrap; run the inspect command below to investigate\n"
	}
}

// defaultBinaryLocation returns the path of the running executable so the
// installed service points at the same binary the user invoked, falling back
// to defaultBinaryPath if it can't be resolved.
func defaultBinaryLocation() string {
	exe, err := os.Executable()
	if err != nil {
		return defaultBinaryPath
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// resolveServicePath returns the trimmed value (or the fallback when empty) with
// a leading "~" expanded to the user's home directory.
func resolveServicePath(value, fallback, home string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		v = fallback
	}
	return expandHome(v, home)
}

// expandHome expands a leading "~" or "~/" in path to the given home directory.
func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// renderLaunchdPlist builds the launchd agent plist. Path values are XML-escaped
// so a path containing "&" or "<" cannot corrupt the document.
//
// WorkingDirectory is set to the config file's directory (an app-specific,
// writable location). Without it, launchd runs the service from "/", so
// relative config defaults such as Storage.Dir "./storage" would resolve to
// "/storage" (unwritable) whenever mowa starts with no config file.
//
// When port is non-empty it is exported as MOWA_PORT via EnvironmentVariables so
// the installed service listens on the same port the user runs mowa with;
// otherwise the key is omitted and mowa uses its built-in default.
func renderLaunchdPlist(binaryPath, configPath, stdoutLog, stderrLog, port string) string {
	workingDir := filepath.Dir(configPath)

	envSection := ""
	if port != "" {
		envSection = fmt.Sprintf(`    <key>EnvironmentVariables</key>
    <dict>
        <key>MOWA_PORT</key>
        <string>%s</string>
    </dict>
`, html.EscapeString(port))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>-config</string>
        <string>%s</string>
    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
%s    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
</dict>
</plist>
`,
		html.EscapeString(launchdLabel),
		html.EscapeString(binaryPath),
		html.EscapeString(configPath),
		html.EscapeString(workingDir),
		envSection,
		html.EscapeString(stdoutLog),
		html.EscapeString(stderrLog),
	)
}

// writeFileAtomic writes data to a temp file in the destination directory and
// renames it into place, so an interruption (crash, full disk) can never leave
// a partially-written plist that would make a later `launchctl bootstrap`
// failure harder to diagnose. The temp file's contents are fsynced before the
// rename and the parent directory is fsynced after, so the write is durable
// across a crash or power loss rather than only atomic. The temp file is removed
// on any error before the rename.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail out before the rename succeeds.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Flush contents to disk before the rename so a crash can't leave the
	// renamed file with unwritten (empty/truncated) contents.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// fsync the directory so the rename itself survives a crash.
	return syncDir(filepath.Dir(path))
}

// syncDir fsyncs a directory so a rename into it is durable. A directory that
// cannot be opened or synced (some filesystems disallow it) is treated as a
// non-fatal best effort.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		// Not all filesystems support directory fsync; the rename already
		// succeeded, so don't fail the install over durability best-effort.
		return nil
	}
	return nil
}

// ensureExecutable verifies that path points to an existing, non-directory file
// with at least one executable bit set, so `mowa install` rejects a bad binary
// path up front instead of letting launchd fail to start the service later.
func ensureExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("mowa binary %s is not accessible: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("mowa binary %s is a directory, not an executable", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("mowa binary %s is not executable", path)
	}
	return nil
}

// servicePID reports whether the service target is loaded and, if it is running,
// its pid. A non-zero launchctl exit means the service is not loaded. A loaded
// but idle service returns (0, true).
func servicePID(serviceTarget string) (int, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), launchctlTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "launchctl", "print", serviceTarget).CombinedOutput()
	if err != nil {
		return 0, false
	}
	m := pidLineRegexp.FindSubmatch(out)
	if m == nil {
		return 0, true
	}
	pid, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0, true
	}
	return pid, true
}

// runLaunchctl runs a launchctl subcommand with a bounded deadline and returns
// its trimmed combined output and any error.
func runLaunchctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), launchctlTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	output := strings.TrimSpace(string(out))

	joined := strings.Join(args, " ")
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("launchctl %s timed out after %s", joined, launchctlTimeout)
	}
	if err != nil {
		return output, fmt.Errorf("launchctl %s failed: %w", joined, err)
	}
	return output, nil
}

// indent prefixes every line of s with two spaces for readable nested output.
func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
