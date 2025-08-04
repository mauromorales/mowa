package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// This is not an ideal test, but all I care about right now is to check that i don't remove any endpoints by mistake, hence why it's so simple.
func TestEndToEnd(t *testing.T) {
	// Start the server in background
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer cmd.Process.Kill()
	
	// Wait a bit for server to start
	time.Sleep(2 * time.Second)
	
	// Test health check endpoint
	resp, err := http.Get("http://localhost:8080/")
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
	
	body := make([]byte, 1024)
	n, _ := resp.Body.Read(body)
	responseBody := string(body[:n])
	
	// Check that all expected endpoints are mentioned
	expectedEndpoints := []string{
		"POST /api/messages",
		"GET /api/uptime",
		"GET/POST /api/storage",
	}
	
	for _, endpoint := range expectedEndpoints {
		if !strings.Contains(responseBody, endpoint) {
			t.Errorf("Health check should mention endpoint: %s", endpoint)
		}
	}
	
	fmt.Printf("✅ All endpoints found in health check response\n")
} 