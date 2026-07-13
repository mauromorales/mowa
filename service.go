package main

import (
	"context"
	"flag"
	"fmt"
	"html"
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
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	binaryFlag := fs.String("binary", "", "Path to the mowa binary launchd should run (default: the running executable)")
	configFlag := fs.String("config", "", "Path to the config file passed via -config (default: ~/Library/Application Support/mowa/config.yaml)")
	stdoutFlag := fs.String("stdout", "", "Path for the service's stdout log (default: ~/Library/Logs/mowa.out)")
	stderrFlag := fs.String("stderr", "", "Path for the service's stderr log (default: ~/Library/Logs/mowa.err)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mowa install [flags]\n\nInstalls mowa as a launchd login service. All flags are optional.\nA leading \"~\" in any path is expanded to your home directory.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not resolve the current user's home directory: %w", err)
	}

	binaryPath := resolveServicePath(*binaryFlag, defaultBinaryLocation(), home)
	configPath := resolveServicePath(*configFlag, filepath.Join(home, "Library", "Application Support", "mowa", "config.yaml"), home)
	stdoutLog := resolveServicePath(*stdoutFlag, filepath.Join(home, "Library", "Logs", "mowa.out"), home)
	stderrLog := resolveServicePath(*stderrFlag, filepath.Join(home, "Library", "Logs", "mowa.err"), home)

	// Ensure the directories launchd and the config file need exist. The config
	// file itself is left absent on purpose: loadConfig falls back to defaults
	// when it is missing, so we must not create or overwrite it here.
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	for _, dir := range []string{launchAgentsDir, filepath.Dir(configPath), filepath.Dir(stdoutLog), filepath.Dir(stderrLog)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	plistPath := filepath.Join(launchAgentsDir, launchdLabel+".plist")
	if err := os.WriteFile(plistPath, []byte(renderLaunchdPlist(binaryPath, configPath, stdoutLog, stderrLog)), 0644); err != nil {
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

	fmt.Printf("✅ Installed and started %s\n", launchdLabel)
	fmt.Printf("   binary: %s\n", binaryPath)
	fmt.Printf("   config: %s\n", configPath)
	fmt.Println("   The service will start automatically at login and stay alive (KeepAlive).")
	fmt.Printf("   Check it with: launchctl print %s\n", serviceTarget)
	return nil
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
func renderLaunchdPlist(binaryPath, configPath, stdoutLog, stderrLog string) string {
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
    <key>RunAtLoad</key>
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
		html.EscapeString(stdoutLog),
		html.EscapeString(stderrLog),
	)
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
