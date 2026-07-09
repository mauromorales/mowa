.PHONY: build run clean test deps

# Build the application
build: generate-docs
	go build -ldflags="-s -w" -o mowa

# Generate Swagger documentation
generate-docs:
	@echo "🔄 Generating Swagger documentation..."
	@chmod +x scripts/generate-docs.sh
	@./scripts/generate-docs.sh

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

# Run tests
test:
	go test ./...

# Build for different platforms
build-all: generate-docs
	go build -ldflags="-s -w" -o mowa

# Development mode with hot reload (requires air)
dev:
	air

# Install air for hot reload (optional)
install-air:
	go install github.com/cosmtrek/air@latest 