package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func setupTestServer() *echo.Echo {
	e := echo.New()
	
	// Setup routes
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Mowa API is running! 🚀\n\nAvailable endpoints:\n- POST /api/messages\n- GET /api/uptime")
	})
	
	api := e.Group("/api")
	api.GET("/uptime", handleGetUptime)
	api.POST("/messages", handleSendMessages)
	
	return e
}

func TestUptimeEndpoint(t *testing.T) {
	e := setupTestServer()
	
	req := httptest.NewRequest(http.MethodGet, "/api/uptime", nil)
	rec := httptest.NewRecorder()
	
	e.ServeHTTP(rec, req)
	
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	
	var response UptimeResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	
	// Test that response has required fields
	if response.Uptime == "" {
		t.Error("Expected uptime field to be non-empty")
	}
	if response.UptimeSeconds <= 0 {
		t.Errorf("Expected uptimeSeconds > 0, got %f", response.UptimeSeconds)
	}
	if response.Formatted == "" {
		t.Error("Expected formatted field to be non-empty")
	}
}

func TestMessagesEndpoint(t *testing.T) {
	e := setupTestServer()
	
	// Test valid message request
	validRequest := `{
		"to": ["+1234567890"],
		"message": "Test message from Mowa"
	}`
	
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(validRequest))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	
	e.ServeHTTP(rec, req)
	
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	
	var response MessageResponse
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	
	// Test that response has results array with correct structure
	if len(response.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(response.Results))
	}
	
	result := response.Results[0]
	if result.Recipient != "+1234567890" {
		t.Errorf("Expected recipient +1234567890, got %s", result.Recipient)
	}
	
	// Note: We can't test success/failure without AppleScript, but we can test the structure
	// The actual AppleScript execution will fail in CI, but the endpoint should still return a response
}

func TestMessagesEndpointInvalidRequest(t *testing.T) {
	e := setupTestServer()
	
	// Test invalid JSON
	invalidRequest := `{
		"to": ["+1234567890"],
		"message": "Test message"
	` // Missing closing brace
	
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(invalidRequest))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	
	e.ServeHTTP(rec, req)
	
	// Should return 400 for invalid JSON
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestMessagesEndpointEmptyRecipients(t *testing.T) {
	e := setupTestServer()
	
	// Test with empty recipients array
	request := `{
		"to": [],
		"message": "Test message"
	}`
	
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(request))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	
	e.ServeHTTP(rec, req)
	
	// Should return 400 for empty recipients (validation error)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHealthCheckEndpoint(t *testing.T) {
	e := setupTestServer()
	
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	
	e.ServeHTTP(rec, req)
	
	// Health check should return 200 OK
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
} 