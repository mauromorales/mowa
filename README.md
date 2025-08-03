# Mowa - macOS Web API

A Go-native web API server that allows you to interact with macOS and iCloud features remotely via HTTP. Built with Echo framework for maximum performance and easy deployment.

## Features

- **Send Messages**: Send iMessages via the Messages app using AppleScript
- **System Uptime**: Get system uptime using shell commands
- **Modular Architecture**: Easy to extend with new endpoints for volume control, app launching, etc.
- **Go Native**: Single binary deployment, no external runtimes required
- **High Performance**: Compiled Go with Echo web framework

## Requirements

- macOS 10.15 or later
- Go 1.21 or later

## Quick Start

### Option 1: Download Pre-built Binary (Recommended)

Download the latest release from [GitHub Releases](https://github.com/your-username/mowa/releases) and extract it:

```bash
# Download and extract (replace with actual release URL)
curl -L https://github.com/your-username/mowa/releases/latest/download/mowa_Darwin_x86_64.zip -o mowa.zip
unzip mowa.zip
chmod +x mowa

# Run the server
./mowa
```

### Option 2: Build from Source

```bash
# Clone the repository
git clone <your-repo-url>
cd mowa

# Build the project
go build -o mowa

# Run the server
./mowa
```

### 2. Test the API

The server will start on `http://localhost:8080` by default. You can test it with:

```bash
# Health check
curl http://localhost:8080

# Get system uptime
curl http://localhost:8080/api/uptime

# Send a message (replace with real phone numbers)
curl -X POST http://localhost:8080/api/messages \
  -H "Content-Type: application/json" \
  -d '{
    "to": ["+1234567890"],
    "message": "Hello from Mowa!"
  }'
```

## API Endpoints

### GET /
Health check endpoint that returns available endpoints.

### GET /api/uptime
Returns system uptime information.

**Response:**
```json
{
  "uptime": "2 days, 3 hours, 45 minutes",
  "uptimeSeconds": 183900.0,
  "formatted": "2 days, 3 hours, 45 minutes"
}
```

### POST /api/messages
Send messages via the Messages app.

**Request:**
```json
{
  "to": ["+1234567890", "+999999999"],
  "message": "Hello World!"
}
```

**Response:**
```json
{
  "results": [
    {
      "recipient": "+1234567890",
      "success": true,
      "error": null
    },
    {
      "recipient": "+999999999",
      "success": false,
      "error": "Invalid phone number: Phone number must start with +"
    }
  ]
}
```

## Development Setup

### Prerequisites

1. **Install Go** (if not already installed):
   ```bash
   # Using Homebrew
   brew install go
   
   # Or download from https://golang.org/dl/
   ```

2. **Verify Go installation**:
   ```bash
   go version
   ```

### Building from Source

1. **Clone the repository**:
   ```bash
   git clone <your-repo-url>
   cd mowa
   ```

2. **Install dependencies**:
   ```bash
   go mod tidy
   ```

3. **Build the project**:
   ```bash
   # Development build
   go build -o mowa
   
   # Release build
   go build -ldflags="-s -w" -o mowa
   ```

4. **Run the server**:
   ```bash
   # Development mode
   go run .
   
   # Release mode
   ./mowa
   
   # With custom port
   MOWA_PORT=3000 go run .
   ```

## Project Structure

```
mowa/
├── go.mod                 # Go module file
├── go.sum                 # Go dependencies checksum
├── main.go               # Application entry point
├── models.go             # Data models and structures
├── messages.go           # Message sending logic
├── uptime.go             # System operations
└── README.md             # This file
```

## Configuration

### Environment Variables

- **MOWA_PORT**: Set the port number for the server (default: 8080)
  ```bash
  # Use port 3000
  MOWA_PORT=3000 go run .
  
  # Use port 9000
  MOWA_PORT=9000 ./mowa
  ```

## Architecture

The project follows a clean, modular architecture:

- **Models**: Define the structure of API requests and responses
- **Handlers**: Handle HTTP endpoints and request processing
- **Services**: Handle business logic and system interactions
- **Main**: Application configuration and startup

### Key Components

1. **Echo Framework**: High-performance, minimalist HTTP web framework
2. **AppleScript Integration**: Uses osascript for Messages app control
3. **Error Handling**: Comprehensive error handling with proper HTTP status codes
4. **CORS Support**: Enabled for web browser access

## Extending the API

To add new endpoints, follow this pattern:

1. **Add models** in `models.go`:
   ```go
   type NewRequest struct {
       Parameter string `json:"parameter" binding:"required"`
   }
   
   type NewResponse struct {
       Result string `json:"result"`
   }
   ```

2. **Add handler** in a new file or existing file:
   ```go
   func handleNewEndpoint(c *gin.Context) {
       var request NewRequest
       if err := c.ShouldBindJSON(&request); err != nil {
           c.JSON(400, gin.H{"error": err.Error()})
           return
       }
       
       // Implementation
       result := doSomething(request.Parameter)
       
       c.JSON(200, NewResponse{Result: result})
   }
   ```

3. **Add the route** in `main.go`:
   ```go
   api.POST("/new-endpoint", handleNewEndpoint)
   ```

## Deployment

### Single Binary Deployment

```bash
# Build for your platform
go build -ldflags="-s -w" -o mowa

# Deploy the single binary
./mowa
```



## Troubleshooting

### Common Issues

1. **Permission Denied**: Make sure the Messages app has necessary permissions
2. **AppleScript Errors**: Verify that the Messages app is installed and accessible
3. **Port Already in Use**: Change the port using MOWA_PORT environment variable

### Debug Mode

Run in debug mode for detailed logging:
```bash
go run .
```

## Security Considerations

- The API runs on localhost only by default
- No authentication is implemented (add as needed for production)
- AppleScript execution requires user interaction for Messages app
- Consider implementing rate limiting for production use

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.
