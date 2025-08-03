package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Get port from environment variable or use default 8080
	port := getPort()

	log.Printf("🚀 Mowa server starting on http://localhost:%d", port)

	// Create Echo instance
	e := echo.New()

	// Custom logger configuration for nicer output
	loggerConfig := middleware.LoggerConfig{
		Format: "${time_rfc3339} | ${status} | ${latency} | ${remote_ip} | ${method} ${uri}\n",
		CustomTimeFormat: "2006/01/02 15:04:05",
	}

	// Middleware
	e.Use(middleware.LoggerWithConfig(loggerConfig))
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	// Health check endpoint
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Mowa API is running! 🚀\n\nAvailable endpoints:\n- POST /api/messages\n- GET /api/uptime")
	})

	// API routes
	api := e.Group("/api")
	{
		// Messages endpoint
		api.POST("/messages", handleSendMessages)
		
		// Uptime endpoint
		api.GET("/uptime", handleGetUptime)
	}

	// Start server
	log.Fatal(e.Start(":" + strconv.Itoa(port)))
}

// getPort returns the port from environment variable or default 8080
func getPort() int {
	portStr := os.Getenv("MOWA_PORT")
	if portStr == "" {
		return 8080
	}
	
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Printf("Invalid port %s, using default 8080", portStr)
		return 8080
	}
	
	return port
} 