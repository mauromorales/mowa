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

	return processStorageRequest(c, path, "")
}

// processStorageRequest handles the common logic for storage operations
func processStorageRequest(c echo.Context, path string, content string) error {
	// Validate path to prevent directory traversal attacks
	if !isValidPath(path) {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "invalid path: contains forbidden characters or directory traversal",
		})
	}

	// Construct full file path
	fullPath := filepath.Join(appConfig.Storage.Dir, path)

	// Ensure the path is within the storage directory
	storageDir, err := filepath.Abs(appConfig.Storage.Dir)
	if err != nil {
		log.Printf("Failed to resolve storage directory %s: %v", appConfig.Storage.Dir, err)
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   "internal server error",
		})
	}

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		log.Printf("Failed to resolve file path %s: %v", fullPath, err)
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   "internal server error",
		})
	}

	if !strings.HasPrefix(absFullPath, storageDir) {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "path is outside of storage directory",
		})
	}

	// Handle based on HTTP method
	switch c.Request().Method {
	case http.MethodGet:
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

// handleGetFile retrieves a file from storage
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
		return c.JSON(http.StatusNotFound, StorageResponse{
			Success: false,
			Error:   "file not found",
		})
	}

	return c.JSON(http.StatusOK, StorageResponse{
		Success: true,
		Content: string(content),
	})
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
