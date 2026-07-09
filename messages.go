package main

import (
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
)

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

	// Create AppleScript to send message via Messages app
	script := fmt.Sprintf(`
tell application "Messages"
    set targetService to 1st service whose service type = iMessage
    set myBuddy to buddy "%s" of targetService
    send "%s" to myBuddy
end tell
`, recipient, escapedMessage)

	// Execute the AppleScript
	return executeAppleScript(script)
}

// executeAppleScript executes an AppleScript and returns any error
func executeAppleScript(script string) error {
	cmd := exec.Command("osascript", "-e", script)

	output, err := cmd.CombinedOutput()
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
