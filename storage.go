package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// handleStorage handles both GET and POST requests for storage operations
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

	return processStorageRequest(c, req.Path, req.Content)
}

// handleStorageWithPath handles storage requests where the path is provided in the URL
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
func processStorageRequest(c echo.Context, path string, content string) error {
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
		return handleGetFile(c, absFullPath)
	case http.MethodPost:
		return handleSaveFile(c, absFullPath, content)
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
func handleGetFile(c echo.Context, fullPath string) error {
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return echo.NewHTTPError(http.StatusNotFound, "file not found")
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		// Log the real error for debugging, but don't expose it to the client
		log.Printf("Failed to read file %s: %v", fullPath, err)

		// Since we already checked that the file exists with os.Stat(),
		// any read error is likely due to permissions, I/O issues, etc.
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   "failed to read file",
		})
	}

	// Return the actual file content in a structured response
	return c.JSON(http.StatusOK, StorageResponse{
		Success: true,
		Content: string(content),
	})
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
func handleSaveFile(c echo.Context, fullPath string, content string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Failed to create directory %s: %v", dir, err)
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   "failed to save file",
		})
	}

	// Write file content
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		log.Printf("Failed to write file %s: %v", fullPath, err)
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   "failed to save file",
		})
	}

	return c.JSON(http.StatusOK, StorageResponse{
		Success: true,
		Content: "File saved successfully",
	})
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
