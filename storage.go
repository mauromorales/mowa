package main

import (
	"fmt"
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
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid request body: %v", err),
		})
	}
	
	if req.Path == "" {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "path is required",
		})
	}

	// Validate path to prevent directory traversal attacks
	if !isValidPath(req.Path) {
		return c.JSON(http.StatusBadRequest, StorageResponse{
			Success: false,
			Error:   "invalid path: contains forbidden characters or directory traversal",
		})
	}

	// Construct full file path
	fullPath := filepath.Join(appConfig.Storage.Dir, req.Path)
	
	// Ensure the path is within the storage directory
	storageDir, err := filepath.Abs(appConfig.Storage.Dir)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve storage directory: %v", err),
		})
	}
	
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve file path: %v", err),
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
		return handleSaveFile(c, absFullPath, req.Content)
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
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to read file: %v", err),
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
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create directory: %v", err),
		})
	}

	// Write file content
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return c.JSON(http.StatusInternalServerError, StorageResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to write file: %v", err),
		})
	}

	return c.JSON(http.StatusOK, StorageResponse{
		Success: true,
		Content: fmt.Sprintf("File saved successfully to %s", fullPath),
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