package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Parse command line flags
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to configuration file (optional)")
	flag.Parse()

	// Load configuration
	var err error
	appConfig, err = loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Get port from environment variable or use default 8080
	port := getPort()

	log.Printf("🚀 Mowa server starting on http://localhost:%d", port)

	// Create Echo instance
	e := echo.New()

	// Custom logger configuration for nicer output
	loggerConfig := middleware.LoggerConfig{
		Format:           "${time_rfc3339} | ${status} | ${latency} | ${remote_ip} | ${method} ${uri}\n",
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
		response := `Mowa API is running! 🚀

Available endpoints:
- POST /api/messages
- GET /api/uptime
- GET/POST /api/storage (JSON payload: returns structured response with file content)
- GET /api/storage/* (URL path: returns raw file content)`
		return c.String(http.StatusOK, response)
	})

	// API routes
	api := e.Group("/api")
	{
		// Messages endpoint
		api.POST("/messages", handleSendMessages)

		// Uptime endpoint
		api.GET("/uptime", handleGetUptime)

		// Storage endpoint (GET and POST) - supports both JSON payload and URL path
		api.GET("/storage", handleStorage)
		api.POST("/storage", handleStorage)

		// Storage endpoint with path in URL (GET only)
		api.GET("/storage/*", handleStorageWithPath)
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
