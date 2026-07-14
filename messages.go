package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// defaultSendTimeoutSeconds bounds a single osascript send. It is intentionally
// well under the ~120s default AppleEvent timeout so a wedged Messages bridge
// fails fast instead of hanging the HTTP request. Kept below the doorbell
// client's 10s read timeout (7 + 2s grace = 9s) so mowa fails before the client
// gives up.
const defaultSendTimeoutSeconds = 7

// @Summary Send messages to recipients
// @Description Send messages to one or more recipients via iMessage
// @Tags messages
// @Accept json
// @Produce json
// @Param request body MessageRequest true "Message request"
// @Success 200 {object} MessageResponse "Messages sent successfully"
// @Failure 400 {object} map[string]interface{} "Bad request - invalid input"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /api/messages [post]
func handleSendMessages(c echo.Context) error {
	var request MessageRequest
	if err := c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error":   "Invalid request format",
			"details": err.Error(),
		})
	}

	// Validate required fields
	if len(request.To) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "At least one recipient is required",
		})
	}

	if request.Message == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Message content is required",
		})
	}

	// Expand groups to individual recipients
	expandedRecipients := expandGroups(request.To)

	// Send messages to all recipients
	results := sendMessages(expandedRecipients, request.Message)

	// Return results
	return c.JSON(http.StatusOK, MessageResponse{Results: results})
}

// sendMessages sends messages to multiple recipients
func sendMessages(recipients []string, message string) []MessageResult {
	var results []MessageResult

	for _, recipient := range recipients {
		result := MessageResult{
			Recipient: recipient,
			Success:   false,
		}

		// Validate phone number
		if err := validatePhoneNumber(recipient); err != nil {
			errorMsg := err.Error()
			result.Error = &errorMsg
			results = append(results, result)
			continue
		}

		// Send the message
		if err := sendMessage(recipient, message); err != nil {
			errorMsg := err.Error()
			result.Error = &errorMsg
		} else {
			result.Success = true
		}

		results = append(results, result)
	}

	return results
}

// sendMessage sends a single message to one recipient
func sendMessage(recipient, message string) error {
	// Escape the message content for AppleScript
	escapedMessage := strings.ReplaceAll(message, "\"", "\\\"")

	timeout := sendTimeout()

	// Create AppleScript to send message via Messages app. The `with timeout`
	// block makes the AppleEvent surface a clean error faster than its ~120s
	// default; executeAppleScript enforces a hard deadline as a backstop.
	script := fmt.Sprintf(`
with timeout of %d seconds
    tell application "Messages"
        set targetService to 1st service whose service type = iMessage
        set myBuddy to buddy "%s" of targetService
        send "%s" to myBuddy
    end tell
end timeout
`, int(timeout.Seconds()), recipient, escapedMessage)

	// Execute the AppleScript
	return executeAppleScript(script, timeout)
}

// sendTimeout returns the configured osascript send timeout, falling back to
// the default when no config has been loaded or the value is invalid.
func sendTimeout() time.Duration {
	if appConfig != nil && appConfig.Messages.TimeoutSeconds > 0 {
		return time.Duration(appConfig.Messages.TimeoutSeconds) * time.Second
	}
	return defaultSendTimeoutSeconds * time.Second
}

// runOSAScript invokes osascript with the given arguments under a bounded
// deadline, killing the process on timeout so no orphaned osascript lingers.
// It returns the combined output, whether the deadline was exceeded, and any
// exec error. This is the shared low-level runner used by both the Messages
// AppleScript path (executeAppleScript) and the Reminders JXA path.
func runOSAScript(timeout time.Duration, args ...string) (output []byte, timedOut bool, err error) {
	// Give the process a small grace period beyond any in-script `with timeout`
	// so its cleaner error can surface before the hard kill.
	ctx, cancel := context.WithTimeout(context.Background(), timeout+2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "osascript", args...)

	output, err = cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, true, fmt.Errorf("osascript timed out after %s", timeout)
	}
	return output, false, err
}

// executeAppleScript executes an AppleScript with a bounded deadline and returns
// any error. The process is killed on timeout so no orphaned osascript lingers.
func executeAppleScript(script string, timeout time.Duration) error {
	output, timedOut, err := runOSAScript(timeout, "-e", script)
	if timedOut {
		log.Printf("AppleScript timed out after %s; killed osascript", timeout)
		return err
	}
	if err != nil {
		log.Printf("AppleScript failed with error: %v", err)
		log.Printf("AppleScript output: %s", string(output))
		log.Printf("Failed script: %s", script)
		return fmt.Errorf("AppleScript error: %s", string(output))
	}

	if len(output) > 0 {
		log.Printf("AppleScript output: %s", string(output))
	}

	return nil
}

// validatePhoneNumber validates phone number format
func validatePhoneNumber(phoneNumber string) error {
	// Remove spaces
	cleanNumber := strings.ReplaceAll(phoneNumber, " ", "")

	// Check if it starts with +
	if !strings.HasPrefix(cleanNumber, "+") {
		return fmt.Errorf("phone number must start with +")
	}

	// Get digits only
	digitsOnly := strings.TrimPrefix(cleanNumber, "+")

	// Check if it contains only digits
	matched, _ := regexp.MatchString(`^\d+$`, digitsOnly)
	if !matched {
		return fmt.Errorf("phone number can only contain digits after the +")
	}

	// Check minimum length
	if len(digitsOnly) < 10 {
		return fmt.Errorf("phone number must be at least 10 digits")
	}

	return nil
}
