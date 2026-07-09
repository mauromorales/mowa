package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// @Summary Handle storage operations
// @Description Handle both GET and POST requests for storage operations with JSON payload. Optionally send notifications about operation results via iMessage.
// @Tags storage
// @Accept json
// @Produce json
// @Param request body StorageRequest true "Storage request"
// @Success 200 {object} StorageResponse "Storage operation completed successfully"
// @Failure 400 {object} StorageResponse "Bad request - invalid input"
// @Failure 404 {object} StorageResponse "File not found"
// @Failure 500 {object} StorageResponse "Internal server error"
// @Router /api/storage [get]
// @Router /api/storage [post]
func handleStorage(c echo.Context) error {
	var req StorageRequest

	// Parse JSON body for both GET and POST requests
	if err := c.Bind(&req); err != nil {
		log.Printf("Failed to parse request body: %v", err)
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "invalid request body",
		})
	}

	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "path is required",
		})
	}

	// Validate notify field - if provided, it must not be empty
	if req.Notify != nil && len(req.Notify) == 0 {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "notify field cannot be empty - either omit it or provide at least one recipient",
		})
	}

	return processStorageRequest(c, req.Path, req.Content, req.Notify)
}

// @Summary Handle storage operations with URL path
// @Description Handle GET requests for storage operations where path is provided in URL
// @Tags storage
// @Produce text/plain
// @Param path path string true "File path" default(/example.txt)
// @Success 200 {string} string "File content"
// @Failure 400 {object} StorageResponse "Bad request - invalid path"
// @Failure 404 {object} StorageResponse "File not found"
// @Failure 500 {object} StorageResponse "Internal server error"
// @Router /api/storage/{path} [get]
func handleStorageWithPath(c echo.Context) error {
	// Extract path from URL parameter
	pathParam := c.Param("*")
	if pathParam == "" {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "path is required",
		})
	}

	// Ensure path starts with /
	path := "/" + strings.TrimPrefix(pathParam, "/")
	// Explicitly check for empty normalized path (i.e., "/")
	if path == "/" {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "path is required",
		})
	}

	// Only GET requests are supported for URL path approach
	if c.Request().Method != http.MethodGet {
		return c.JSON(http.StatusMethodNotAllowed, StorageResponse{
			Success: false,
			Error:   "method not allowed - use POST /api/storage with JSON payload for file creation",
		})
	}

	// For URL path approach, return raw file content
	return processStorageRequestRaw(c, path)
}

// validateAndResolvePath validates the path and resolves it to an absolute path within the storage directory
func validateAndResolvePath(path string) (string, error) {
	// Validate path to prevent directory traversal attacks
	if !isValidPath(path) {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid path: contains forbidden characters or directory traversal")
	}

	// Construct full file path
	fullPath := filepath.Join(appConfig.Storage.Dir, path)

	// Ensure the path is within the storage directory
	storageDir, err := filepath.Abs(appConfig.Storage.Dir)
	if err != nil {
		log.Printf("Failed to resolve storage directory %s: %v", appConfig.Storage.Dir, err)
		return "", echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		log.Printf("Failed to resolve file path %s: %v", fullPath, err)
		return "", echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	if !strings.HasPrefix(absFullPath, storageDir) {
		return "", echo.NewHTTPError(http.StatusBadRequest, "path is outside of storage directory")
	}

	return absFullPath, nil
}

// processStorageRequest handles the common logic for storage operations
func processStorageRequest(c echo.Context, path string, content string, notify []string) error {
	absFullPath, err := validateAndResolvePath(path)
	if err != nil {
		// Convert echo.NewHTTPError to JSON response for structured API
		if httpErr, ok := err.(*echo.HTTPError); ok {
			return c.JSON(httpErr.Code, StorageResponse{
				Success: false,
				Error:   httpErr.Message.(string),
			})
		}
		return err
	}

	// Handle based on HTTP method
	switch c.Request().Method {
	case http.MethodGet:
		// Return file content in a structured response
		return handleGetFile(c, absFullPath, notify)
	case http.MethodPost:
		return handleSaveFile(c, absFullPath, content, notify)
	default:
		return c.JSON(http.StatusMethodNotAllowed, StorageResponse{
			Success: false,
			Error:   "method not allowed",
		})
	}
}

// processStorageRequestRaw handles the common logic for raw file access
func processStorageRequestRaw(c echo.Context, path string) error {
	absFullPath, err := validateAndResolvePath(path)
	if err != nil {
		// For raw file access, return the error directly (it's already an echo.NewHTTPError)
		return err
	}

	// Return raw file content
	return handleGetFileRaw(c, absFullPath)
}

// handleGetFile retrieves a file from storage and returns a structured response
func handleGetFile(c echo.Context, fullPath string, notify []string) error {
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		// Send notification if requested
		if len(notify) > 0 {
			go sendStorageNotification(notify, "GET", fullPath, false, "find file")
		}
		return echo.NewHTTPError(http.StatusNotFound, "file not found")
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		// Log the real error for debugging, but don't expose it to the client
		log.Printf("Failed to read file %s: %v", fullPath, err)

		// Since we already checked that the file exists with os.Stat(),
		// any read error is likely due to permissions, I/O issues, etc.
		response := StorageResponse{
			Success: false,
			Error:   "failed to read file",
		}

		// Send notification if requested
		if len(notify) > 0 {
			go sendStorageNotification(notify, "GET", fullPath, false, "read file")
		}

		return c.JSON(http.StatusInternalServerError, response)
	}

	// Return the actual file content in a structured response
	response := StorageResponse{
		Success: true,
		Content: string(content),
	}

	// Send notification if requested
	if len(notify) > 0 {
		go sendStorageNotification(notify, "GET", fullPath, true, "retrieved successfully")
	}

	return c.JSON(http.StatusOK, response)
}

// handleGetFileRaw retrieves a file from storage and returns just the content
func handleGetFileRaw(c echo.Context, fullPath string) error {
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "file not found")
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		// Log the real error for debugging, but don't expose it to the client
		log.Printf("Failed to read file %s: %v", fullPath, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read file")
	}

	// Return just the file content
	return c.String(http.StatusOK, string(content))
}

// handleSaveFile saves a file to storage
func handleSaveFile(c echo.Context, fullPath string, content string, notify []string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Failed to create directory %s: %v", dir, err)
		response := StorageResponse{
			Success: false,
			Error:   "failed to save file",
		}

		// Send notification if requested
		if len(notify) > 0 {
			go sendStorageNotification(notify, "POST", fullPath, false, "create directory")
		}

		return c.JSON(http.StatusInternalServerError, response)
	}

	// Write file content
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		log.Printf("Failed to write file %s: %v", fullPath, err)
		response := StorageResponse{
			Success: false,
			Error:   "failed to save file",
		}

		// Send notification if requested
		if len(notify) > 0 {
			go sendStorageNotification(notify, "POST", fullPath, false, "write file")
		}

		return c.JSON(http.StatusInternalServerError, response)
	}

	response := StorageResponse{
		Success: true,
		Content: "File saved successfully",
	}

	// Send notification if requested
	if len(notify) > 0 {
		go sendStorageNotification(notify, "POST", fullPath, true, "saved successfully")
	}

	return c.JSON(http.StatusOK, response)
}

// isValidPath validates that the path doesn't contain dangerous characters or directory traversal
func isValidPath(path string) bool {

	// Check for directory traversal attempts
	if strings.Contains(path, "..") {
		return false
	}

	// Ensure path starts with a forward slash
	if !strings.HasPrefix(path, "/") {
		return false
	}

	return true
}

// sendStorageNotification sends a notification about storage operations
func sendStorageNotification(notify []string, operation string, filePath string, success bool, message string) {
	if len(notify) == 0 {
		return
	}

	// Expand groups to individual recipients
	expandedRecipients := expandGroups(notify)

	// Create notification message
	var notificationMessage string
	fileName := filepath.Base(filePath)
	if success {
		notificationMessage = fmt.Sprintf("%s %s", fileName, message)
	} else {
		notificationMessage = fmt.Sprintf("Failed to %s %s: %s", operation, fileName, message)
	}

	// Send messages to all recipients
	results := sendMessages(expandedRecipients, notificationMessage)

	// Log the notification results
	for _, result := range results {
		if result.Success {
			log.Printf("Storage notification sent successfully to %s", result.Recipient)
		} else {
			log.Printf("Failed to send storage notification to %s: %s", result.Recipient, *result.Error)
		}
	}
}
