package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// launchd service constants. The label is fixed so the plist, the launchctl
// service target, and the self-restart logic all agree on the same identity.
const (
	launchdLabel = "com.mauromorales.mowa"

	// defaultBinaryPath is where a `brew`/manual install typically drops mowa.
	defaultBinaryPath = "/usr/local/bin/mowa"

	// launchctlTimeout bounds each launchctl invocation so a wedged launchd
	// can't hang the HTTP request.
	launchctlTimeout = 10 * time.Second

	// serviceRestartDelay gives the HTTP response a moment to flush before the
	// service is restarted (via kickstart) when this very process is the one
	// launchd manages.
	serviceRestartDelay = 500 * time.Millisecond
)

// pidLineRegexp extracts the running pid from `launchctl print` output, whose
// service dump contains a line like "\tpid = 1234".
var pidLineRegexp = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)

// @Summary Install and start mowa as a launchd service
// @Description Generates the launchd agent plist for mowa, writes it to ~/Library/LaunchAgents/com.mauromorales.mowa.plist (overwriting any existing one), and loads + starts the service via launchctl so mowa runs at login and stays alive (KeepAlive).
// @Description
// @Description All four fields are optional; omitted fields fall back to the defaults shown. A leading "~" in any path is expanded to the current user's home directory, and the plist always uses absolute, home-resolved paths.
// @Description
// @Description Idempotent: calling it repeatedly re-writes the plist and reloads the service. If the label is already loaded under a different process it is booted out and bootstrapped again.
// @Description
// @Description Caveats:
// @Description - If the mowa handling this request is itself the launchd-managed instance, the plist is refreshed and the service is restarted via `launchctl kickstart -k` shortly after the response is sent, so the connection may drop as the service restarts.
// @Description - If the mowa handling this request is NOT running under launchd (e.g. started with `make run`), bootstrapping spawns a separate instance under launchd. If both bind the same port, the launchd instance fails to start (and KeepAlive keeps retrying); the response flags this so it is visible instead of looping silently.
// @Tags system
// @Accept json
// @Produce json
// @Param request body ServiceRequest false "Service configuration (empty body uses all defaults)"
// @Success 200 {object} ServiceResponse "Service installed and started (or restarting)"
// @Failure 400 {object} ServiceResponse "Bad request - invalid body"
// @Failure 500 {object} ServiceResponse "Failed to write the plist or load the service"
// @Router /api/service [post]
func handleService(c echo.Context) error {
	var req ServiceRequest
	// An empty body is valid (use all defaults). Echo surfaces that as io.EOF.
	if err := c.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		return c.JSON(http.StatusBadRequest, ServiceResponse{
			Success: false,
			Message: "invalid request body",
			Error:   err.Error(),
		})
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ServiceResponse{
			Success: false,
			Message: "could not resolve the current user's home directory",
			Error:   err.Error(),
		})
	}

	binaryPath := resolveServicePath(req.BinaryPath, defaultBinaryPath, home)
	configPath := resolveServicePath(req.ConfigPath, filepath.Join(home, "Library", "Application Support", "mowa", "config.yaml"), home)
	stdoutLog := resolveServicePath(req.StdoutLog, filepath.Join(home, "Library", "Logs", "mowa.out"), home)
	stderrLog := resolveServicePath(req.StderrLog, filepath.Join(home, "Library", "Logs", "mowa.err"), home)

	// Ensure the directories launchd and the config file need exist. The config
	// file itself is left absent on purpose: loadConfig falls back to defaults
	// when it is missing, so we must not create or overwrite it here.
	launchAgentsDir := filepath.Join(home, "Library", "LaunchAgents")
	for _, dir := range []string{launchAgentsDir, filepath.Dir(configPath), filepath.Dir(stdoutLog), filepath.Dir(stderrLog)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return c.JSON(http.StatusInternalServerError, ServiceResponse{
				Success: false,
				Message: fmt.Sprintf("failed to create directory %s", dir),
				Error:   err.Error(),
			})
		}
	}

	plistPath := filepath.Join(launchAgentsDir, launchdLabel+".plist")
	plist := renderLaunchdPlist(binaryPath, configPath, stdoutLog, stderrLog)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return c.JSON(http.StatusInternalServerError, ServiceResponse{
			Success:   false,
			PlistPath: plistPath,
			Label:     launchdLabel,
			Message:   "failed to write the launchd plist",
			Error:     err.Error(),
		})
	}

	uid := os.Getuid()
	domainTarget := fmt.Sprintf("gui/%d", uid)
	serviceTarget := fmt.Sprintf("gui/%d/%s", uid, launchdLabel)
	selfPID := os.Getpid()

	prevPID, wasLoaded := servicePID(serviceTarget)

	resp := ServiceResponse{PlistPath: plistPath, Label: launchdLabel}

	// Case 1: this very process is the launchd-managed instance. Booting it out
	// would kill us mid-request, so refresh the plist (already done above) and
	// restart in place with kickstart -k after the response has flushed.
	if wasLoaded && prevPID == selfPID {
		resp.Success = true
		resp.Message = "plist updated; this process is the launchd-managed service and will restart to apply changes (the connection may drop)"
		writeErr := c.JSON(http.StatusOK, resp)
		if writeErr != nil {
			log.Printf("failed to write service response, restarting anyway: %v", writeErr)
		}
		log.Printf("🔄 Refreshed launchd plist; restarting %s via kickstart", serviceTarget)
		go func() {
			time.Sleep(serviceRestartDelay)
			if err := exec.Command("launchctl", "kickstart", "-k", serviceTarget).Run(); err != nil {
				log.Printf("kickstart of %s failed: %v", serviceTarget, err)
			}
		}()
		return writeErr
	}

	// Case 2: the label is loaded under a different process (or not loaded at
	// all). Unload any existing instance, then bootstrap from the fresh plist.
	var steps []ServiceStep
	if wasLoaded {
		steps = append(steps, runLaunchctl("bootout", serviceTarget))
	}
	bootstrap := runLaunchctl("bootstrap", domainTarget, plistPath)
	steps = append(steps, bootstrap)
	resp.Steps = steps

	if bootstrap.Error != "" {
		resp.Success = false
		resp.Message = "failed to bootstrap the launchd service"
		resp.Error = bootstrap.Error
		return c.JSON(http.StatusInternalServerError, resp)
	}

	resp.Success = true
	resp.Message = "launchd service installed and started"

	// If the freshly loaded service runs under a different pid than the caller,
	// the caller is not the launchd-managed instance (e.g. a `make run` process).
	// Flag the likely port conflict instead of letting KeepAlive loop silently.
	if newPID, ok := servicePID(serviceTarget); ok && newPID != 0 && newPID != selfPID {
		resp.Message += fmt.Sprintf(
			"; note: the mowa handling this request (pid %d) is not the launchd-managed instance (pid %d) — if both bind the same port the launchd instance will fail to start and KeepAlive will keep retrying",
			selfPID, newPID,
		)
	}

	return c.JSON(http.StatusOK, resp)
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

// runLaunchctl runs a launchctl subcommand with a bounded deadline and captures
// its combined output and any error as a reportable ServiceStep.
func runLaunchctl(args ...string) ServiceStep {
	ctx, cancel := context.WithTimeout(context.Background(), launchctlTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "launchctl", args...)
	out, err := cmd.CombinedOutput()

	step := ServiceStep{
		Command: "launchctl " + strings.Join(args, " "),
		Output:  strings.TrimSpace(string(out)),
	}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		step.Error = fmt.Sprintf("launchctl timed out after %s", launchctlTimeout)
	case err != nil:
		step.Error = err.Error()
	}
	return step
}
