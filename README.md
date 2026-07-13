# mowa - MacOS Web API

![mowa logo](/assets/mowa-logo.png)

A Go-native web API server that allows you to interact with MacOS and iCloud features remotely via HTTP. Built with Echo framework for maximum performance and easy deployment.

## Features

- **Send Messages**: Send iMessages via the Messages app using AppleScript
- **System Uptime**: Get system uptime using shell commands
- **File Storage**: Save and retrieve YAML files with configurable storage directory
- **Login Service**: `mowa install` sets mowa up as a launchd agent that starts at login and stays alive
- **Modular Architecture**: Easy to extend with new endpoints for volume control, app launching, etc.
- **Go Native**: Single binary deployment, no external runtimes required
- **High Performance**: Compiled Go with Echo web framework
- **Embedded Documentation**: Swagger JSON and YAML files are embedded in the binary

## Quick Start

> [!CAUTION]
> When interacting with other applications for the first time e.g. messages, MacOS will open a pop up requesting for permissions. You must grant these in order for mowa to work as expected.

### Option 1: Download Pre-built Binary (Recommended)

Download the latest release from [GitHub Releases](https://github.com/mauromorales/mowa/releases) and extract it:

```bash
# For Apple Silicon Macs
curl -L https://github.com/mauromorales/mowa/releases/latest/download/mowa_Darwin_arm64.zip -o mowa.zip

# For Intel Macs
curl -L https://github.com/mauromorales/mowa/releases/latest/download/mowa_Darwin_x86_64.zip -o mowa.zip

# Extract and set permissions
unzip mowa.zip
chmod +x mowa

# Remove macOS quarantine attribute (required for downloaded binaries)
xattr -d com.apple.quarantine mowa

# Run the server
./mowa

# Or with a configuration file
./mowa -config config.yaml
```

**Important**: On first run, macOS may block mowa from running or accessing other applications. You'll need to:
1. **Allow mowa to run**: Go to System Preferences → Security & Privacy → General, and click "Allow Anyway" for mowa
2. **Grant accessibility permissions**: Go to System Preferences → Security & Privacy → Privacy → Accessibility, and add mowa to the list of allowed applications
3. **Grant automation permissions**: For Messages functionality, go to System Preferences → Security & Privacy → Privacy → Automation, and ensure mowa has access to Messages

### Option 2: Build from Source

```bash
# Clone the repository
git clone <your-repo-url>
cd mowa

# Build the project (includes Swagger documentation generation)
make build

# Or build manually with docs generation
./scripts/generate-docs.sh
go build -o mowa

# Run the server
./mowa

# Or with a configuration file
./mowa -config config.yaml
```

See [Installing as a Service](#installing-as-a-service) below to have mowa start
automatically at login.

### 2. Test the API

The server will start on `http://localhost:8080` by default. You can test it with:

```bash
# Access API documentation (redirects from root)
curl -L http://localhost:8080/

# Or access Swagger UI directly
open http://localhost:8080/swagger/index.html

# Access embedded Swagger documentation (JSON format)
curl http://localhost:8080/swagger/doc.json

# Access embedded Swagger documentation (YAML format)
curl http://localhost:8080/swagger/doc.yaml

# Get system uptime
curl http://localhost:8080/api/uptime

# Send a message (replace with real phone numbers)
curl -X POST http://localhost:8080/api/messages \
  -H "Content-Type: application/json" \
  -d '{
    "to": ["+1234567890"],
    "message": "Hello from Mowa!"
  }'

# Send to a message group (if configured)
curl -X POST http://localhost:8080/api/messages \
  -H "Content-Type: application/json" \
  -d '{
    "to": ["foobar"],
    "message": "Hello from group!"
  }'

# Save a YAML file
curl -X POST http://localhost:8080/api/storage \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/config/database.yaml",
    "content": "database:\n  host: localhost\n  port: 5432"
  }'

# Retrieve a YAML file (JSON payload - returns success message)
curl -X GET http://localhost:8080/api/storage \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/config/database.yaml"
  }'

# Retrieve a YAML file (URL path - returns actual file contents)
curl -X GET http://localhost:8080/api/storage/config/database.yaml
```

## Installing as a Service

Run `mowa install` to set mowa up as a launchd user agent that starts at login
and stays alive (`KeepAlive`):

```bash
mowa install
```

This writes `~/Library/LaunchAgents/com.mauromorales.mowa.plist`, then loads and
starts the service. By default it points the service at the binary you ran
`install` from, uses `~/Library/Application Support/mowa/config.yaml` for
configuration (the file is optional — mowa uses built-in defaults when it is
absent), and logs to `~/Library/Logs/mowa.out` / `mowa.err`. Re-running
`mowa install` reinstalls the service, so it is safe to run again after
upgrading.

Override any of the paths with flags:

```bash
mowa install \
  --binary /usr/local/bin/mowa \
  --config ~/Library/Application\ Support/mowa/config.yaml \
  --stdout ~/Library/Logs/mowa.out \
  --stderr ~/Library/Logs/mowa.err
```

Inspect, restart, or remove the service with `launchctl`:

```bash
launchctl print gui/$(id -u)/com.mauromorales.mowa    # inspect
launchctl kickstart -k gui/$(id -u)/com.mauromorales.mowa  # restart
launchctl bootout gui/$(id -u)/com.mauromorales.mowa  # stop and unload
tail -f ~/Library/Logs/mowa.out                       # view logs
```

Running mowa under launchd is also what lets the `POST /api/update` self-update
endpoint relaunch the process on the new binary.

## API Documentation

### Swagger UI
The API includes comprehensive Swagger documentation that can be accessed at:
```
http://localhost:8080/swagger/index.html
```

**Note**: The root endpoint (`/`) automatically redirects to the Swagger documentation for convenience.

This interactive documentation allows you to:
- Explore all available endpoints
- Test API calls directly from the browser
- View request/response schemas
- See example requests and responses

### Regenerating Documentation
To regenerate the Swagger documentation after making changes to the API:

```bash
# Using the provided script
./scripts/generate-docs.sh

# Or manually
export PATH=$PATH:$(go env GOPATH)/bin
swag init
```

## API Endpoints

### GET /
Root endpoint that redirects to the Swagger documentation at `/swagger/index.html`.

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
Send messages via the Messages app. Supports both individual recipients and predefined groups.

**Request:**
```json
{
  "to": ["+1234567890", "+999999999"],
  "message": "Hello World!"
}
```

**Request with Groups:**
```json
{
  "to": ["foobar", "+1234567890"],
  "message": "Hello from group!"
}
```

If a recipient in the "to" array matches a group name defined in the configuration file, it will be expanded to include all members of that group.

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

### GET /api/storage
Retrieve YAML files from the configured storage directory. Supports two different request formats with different response behaviors.

#### Option 1: JSON Payload Request
**Request:**
```json
{
  "path": "/my/file.yaml"
}
```

**Response:**
```json
{
  "success": true,
  "content": "database:\n  host: localhost\n  port: 5432"
}
```

#### Option 2: URL Path Request
**Request:**
```
GET /api/storage/my/file.yaml
```

**Response:**
```json
{
  "success": true,
  "content": "database:\n  host: localhost\n  port: 5432"
}
```

**Error Response (404 Not Found):**
```json
{
  "message": "file not found"
}
```

**Note:** Both the JSON payload format and the URL path format return file contents, but in different formats. The JSON payload format returns the file contents inside a JSON response, while the URL path format returns the raw file content.

### POST /api/storage
Save YAML files to the configured storage directory. Creates directories automatically if they don't exist.

**Request (JSON payload):**
```json
{
  "path": "/new/config.yaml",
  "content": "database:\n  host: localhost\n  port: 5432\n  name: myapp"
}
```

**Response:**
```json
{
  "success": true,
  "content": "File saved successfully to /path/to/storage/new/config.yaml"
}
```

**Error Response (400 Bad Request):**
```json
{
  "success": false,
  "error": "invalid path: contains forbidden characters or directory traversal"
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

## Configuration

### Command Line Options

- **-config**: Path to configuration file (optional)
  ```bash
  # Run with configuration file
  ./mowa -config config.yaml
  
  # Run without configuration file
  ./mowa
  ```

### Setting Up Message Groups

1. **Create a config file**: Create a `config.yaml` file in your project directory
2. **Define your groups**: Add your contact groups as shown in the format below
3. **Start the server**: Use the `-config` flag to load your configuration

Example setup:
```bash
# Create your config file
touch config.yaml

# Edit with your contacts
nano config.yaml

# Start server with config
./mowa -config config.yaml
```

### Configuration File Format

Create a `config.yaml` file in your project directory to define message groups and storage settings:

```yaml
messages:
  groups:
    foobar:
      - "+1234567890"
      - "contact@examples.com"
    family:
      - "+1987654321"
      - "+1555123456"
    work:
      - "boss@company.com"
      - "team@company.com"
      - "+1555987654"

storage:
  dir: "/Users/foobar/some/path"  # Custom storage directory (optional)
  # Default is "./storage" if not specified
```

**Important**: Replace the phone numbers and email addresses with your actual contacts. The `config.yaml` file is automatically ignored by git (via `.gitignore`) to protect your privacy.

When you send a message with `"to": ["foobar"]`, it will automatically expand to send to all members of the "foobar" group.

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

1. **Permission Denied**: Make sure mowa has been granted the necessary permissions in System Preferences:
   - **General**: Allow mowa to run (Security & Privacy → General)
   - **Accessibility**: Add mowa to allowed applications (Security & Privacy → Privacy → Accessibility)
   - **Automation**: Grant mowa access to Messages (Security & Privacy → Privacy → Automation)
2. **AppleScript Errors**: Verify that the Messages app is installed and accessible, and that mowa has automation permissions
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
