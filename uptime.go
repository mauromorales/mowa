package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// @Summary Get system uptime
// @Description Get the current system uptime information
// @Tags system
// @Produce json
// @Success 200 {object} UptimeResponse "Uptime information retrieved successfully"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /api/uptime [get]
func handleGetUptime(c echo.Context) error {
	uptime, err := getUptime()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"error":   "Failed to get uptime",
			"details": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, uptime)
}

// getUptime gets system uptime using multiple methods
func getUptime() (UptimeResponse, error) {
	// Try native Go method first
	if uptime, err := getNativeUptime(); err == nil {
		return formatUptimeResponse(uptime), nil
	}

	// Fall back to shell command method
	uptime, err := getShellUptime()
	if err != nil {
		return UptimeResponse{}, err
	}

	return formatUptimeResponse(uptime), nil
}

// getNativeUptime gets uptime using Go's time package
func getNativeUptime() (float64, error) {
	// This is a simplified approach - in a real implementation,
	// you might use syscall to get boot time
	// For now, we'll return an error to fall back to shell command
	return 0, fmt.Errorf("native uptime not implemented")
}

// getShellUptime gets uptime using the uptime command
func getShellUptime() (float64, error) {
	cmd := exec.Command("uptime")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to execute uptime command: %v", err)
	}

	return parseUptimeOutput(string(output))
}

// parseUptimeOutput parses the uptime command output
func parseUptimeOutput(output string) (float64, error) {
	// Example output: " 12:34:56 up 2 days, 3:45, 2 users, load average: 1.23, 1.45, 1.67"

	// Look for "up" followed by time information
	upIndex := strings.Index(output, "up")
	if upIndex == -1 {
		return 0, fmt.Errorf("could not parse uptime output: %s", output)
	}

	// Extract the part after "up"
	afterUp := output[upIndex+2:]

	// Split by comma to get the uptime part
	parts := strings.Split(afterUp, ",")
	if len(parts) == 0 {
		return 0, fmt.Errorf("could not parse uptime component")
	}

	uptimeString := strings.TrimSpace(parts[0])
	return parseUptimeString(uptimeString)
}

// parseUptimeString parses uptime string like "2 days, 3:45" or "3:45" or "2 days"
func parseUptimeString(uptimeString string) (float64, error) {
	var totalSeconds float64

	// Check for days
	dayPattern := regexp.MustCompile(`(\d+)\s+day`)
	dayMatches := dayPattern.FindStringSubmatch(uptimeString)
	if len(dayMatches) > 1 {
		if days, err := strconv.Atoi(dayMatches[1]); err == nil {
			totalSeconds += float64(days * 24 * 60 * 60)
		}
	}

	// Check for time format (HH:MM)
	timePattern := regexp.MustCompile(`(\d+):(\d+)`)
	timeMatches := timePattern.FindStringSubmatch(uptimeString)
	if len(timeMatches) > 2 {
		if hours, err := strconv.Atoi(timeMatches[1]); err == nil {
			if minutes, err := strconv.Atoi(timeMatches[2]); err == nil {
				totalSeconds += float64(hours*60*60 + minutes*60)
			}
		}
	}

	return totalSeconds, nil
}

// formatUptimeResponse formats uptime seconds into a human-readable response
func formatUptimeResponse(uptimeSeconds float64) UptimeResponse {
	days := int(uptimeSeconds) / (24 * 60 * 60)
	hours := (int(uptimeSeconds) % (24 * 60 * 60)) / (60 * 60)
	minutes := (int(uptimeSeconds) % (60 * 60)) / 60

	var formatted []string

	if days > 0 {
		dayText := fmt.Sprintf("%d day", days)
		if days != 1 {
			dayText += "s"
		}
		formatted = append(formatted, dayText)
	}

	if hours > 0 {
		hourText := fmt.Sprintf("%d hour", hours)
		if hours != 1 {
			hourText += "s"
		}
		formatted = append(formatted, hourText)
	}

	if minutes > 0 {
		minuteText := fmt.Sprintf("%d minute", minutes)
		if minutes != 1 {
			minuteText += "s"
		}
		formatted = append(formatted, minuteText)
	}

	formattedString := strings.Join(formatted, ", ")

	return UptimeResponse{
		Uptime:        formattedString,
		UptimeSeconds: uptimeSeconds,
		Formatted:     formattedString,
	}
}
