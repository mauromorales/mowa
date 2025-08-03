package main

import (
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







func TestHealthCheckEndpoint(t *testing.T) {
	e := setupTestServer()
	
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	
	e.ServeHTTP(rec, req)
	
	// Health check should return 200 OK
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
	
	// Check that the response contains both endpoints
	body := rec.Body.String()
	if !strings.Contains(body, "POST /api/messages") {
		t.Error("Health check response should list POST /api/messages endpoint")
	}
	if !strings.Contains(body, "GET /api/uptime") {
		t.Error("Health check response should list GET /api/uptime endpoint")
	}
} 