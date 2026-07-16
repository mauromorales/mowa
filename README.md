# mowa - MacOS Web API

![mowa logo](/assets/mowa-logo.png)

A Go-native web API server that allows you to interact with MacOS and iCloud features remotely via HTTP. Built with Echo framework for maximum performance and easy deployment.

## Features

- **Send Messages**: Send iMessages via the Messages app using AppleScript
- **System Uptime**: Get system uptime using shell commands
- **File Storage**: Save and retrieve YAML files with configurable storage directory
- **Reminders**: Manage macOS Reminders lists and reminders (create, list, edit, complete, delete)
- **Login Service**: `mowa install` sets mowa up as a launchd agent that starts at login and stays alive
- **Update Notifications**: a nightly check messages you when a restart-required macOS update is available, so you can keep automatic installs off and install manually
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
3. **Grant automation permissions**: Go to System Preferences → Security & Privacy → Privacy → Automation, and ensure mowa has access to Messages (for messaging) and Reminders (for the reminders endpoints)

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

The service's port follows `MOWA_PORT`: pass `--port` (or have `MOWA_PORT` set in
your environment when you run `install`) and it is written into the plist's
`EnvironmentVariables`. Without either, the service uses mowa's built-in default
(8080) — note the service does **not** inherit a `MOWA_PORT` you export in your
shell later, only what is captured at install time.

Override any of the paths (and the port) with flags:

```bash
mowa install \
  --binary /usr/local/bin/mowa \
  --config ~/Library/Application\ Support/mowa/config.yaml \
  --stdout ~/Library/Logs/mowa.out \
  --stderr ~/Library/Logs/mowa.err \
  --port 3000
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

## macOS Update Notifications

Auto-installed macOS updates reboot into Setup Assistant and de-register
iMessage, silently breaking every send until someone completes the Apple-ID
sign-in. The recommended setup is therefore to **disable automatic macOS update
installs** and let mowa tell you when an update is waiting.

`mowa install` also writes a second, calendar-scheduled agent
(`~/Library/LaunchAgents/com.mauromorales.mowa-update-check.plist`) that runs
`mowa check-updates` daily (03:00 by default). The check runs
`softwareupdate --list` and, when a **restart-required** update (i.e. a macOS
OS update — Safari and Command Line Tools updates stay silent) is newly
available, messages the configured recipients:

```
⬆️ macOS update available on macmini.local — install manually: macOS Tahoe 26.5.2
```

Each update is announced once (tracked in `update-check-state.json` next to the
config file); when everything is up to date, nothing is sent. Notifications are
**off until you configure recipients** in `config.yaml`:

```yaml
software_update_check:
  notify:            # phone numbers or group names, like the messaging endpoints
    - admins
    - "+1234567890"
  schedule: "03:00"  # optional, HH:MM local time (default 03:00)
  # enabled: false   # optional kill switch without removing recipients
```

The server also re-syncs this agent at startup whenever update checks are
enabled in the config, so an existing installation picks the agent up after a
self-update (`POST /api/update`) or a schedule change without re-running
`mowa install` — no root required, everything stays in the per-user launchd
domain. Listing updates does not need root either; only installing them does.

```bash
launchctl print gui/$(id -u)/com.mauromorales.mowa-update-check  # inspect
tail -f ~/Library/Logs/mowa-update-check.out                     # view logs
./mowa check-updates -config config.yaml                         # run once by hand
```

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

### Reminders

Manage the macOS Reminders app. All routes live under `/api/reminders`.

> [!NOTE]
> The first Reminders call triggers a macOS **Automation** permission prompt for controlling Reminders (System Settings → Privacy & Security → Automation). You must allow it for these endpoints to work. Reminders scripting can be slow on large databases; the per-call timeout defaults to 30s and is configurable via `reminders.timeout_seconds`.

Lists are addressed by their stable `id`; reminders are always addressed by their stable `id` (names are not unique). When an id appears in the URL path it must be percent-encoded.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/reminders/lists` | List all lists (`name`, `id`) |
| `POST` | `/api/reminders/lists` | Create a list: `{"name": "..."}` |
| `DELETE` | `/api/reminders/lists/{id}` | Delete a list and its reminders |
| `GET` | `/api/reminders/lists/{id}/reminders` | Reminders in a list; `?completed=true` includes completed ones (default: incomplete only) |
| `POST` | `/api/reminders` | Create: `{"list": "<id or name>", "name": "...", "notes": "...", "due_date": "RFC3339"}` (`notes`/`due_date` optional) |
| `PATCH` | `/api/reminders/{id}` | Update any of `name`, `notes`, `due_date` (RFC3339), `completed` (bool) |
| `DELETE` | `/api/reminders/{id}` | Delete a reminder |

**Create a reminder:**
```json
{
  "list": "Groceries",
  "name": "Buy milk",
  "notes": "Whole milk, 2 liters",
  "due_date": "2026-07-20T09:00:00Z"
}
```

**Response (201 Created):**
```json
{
  "id": "x-apple-reminder://ABC123",
  "name": "Buy milk",
  "notes": "Whole milk, 2 liters",
  "due_date": "2026-07-20T09:00:00Z",
  "completed": false,
  "completion_date": null,
  "list": "Groceries"
}
```

Moving a reminder to another list (the `list` field on `PATCH`) is **not supported** by the macOS Reminders scripting interface and returns `501 Not Implemented`.

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

reminders:
  timeout_seconds: 30  # Max seconds for a single Reminders osascript call (optional)
  # Default is 30; raise it if Reminders is slow on a large database
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
   - **Automation**: Grant mowa access to Messages and Reminders (Security & Privacy → Privacy → Automation)
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
