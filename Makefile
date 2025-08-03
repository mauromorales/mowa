.PHONY: build run clean test deps

# Build the application
build:
	go build -ldflags="-s -w" -o mowa

# Run the application
run:
	go run .

# Run with custom port
run-port:
	MOWA_PORT=3000 go run .

# Install dependencies
deps:
	go mod tidy
	go mod download

# Clean build artifacts
clean:
	rm -f mowa
	rm -f mowa-*

# Run tests
test:
	go test ./...

# Build for different platforms
build-all: build
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o mowa-linux
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o mowa.exe

# Development mode with hot reload (requires air)
dev:
	air

# Install air for hot reload (optional)
install-air:
	go install github.com/cosmtrek/air@latest 