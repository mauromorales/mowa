#!/bin/bash

# Generate Swagger documentation for Mowa API
echo "🔄 Generating Swagger documentation..."

# Add GOPATH/bin to PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Generate docs
swag init

if [ $? -eq 0 ]; then
    echo "✅ Swagger documentation generated successfully!"
    echo "📁 Files created in docs/ directory:"
    ls -la docs/
    echo ""
    echo "🌐 Access the API documentation at: http://localhost:8080/swagger/index.html"
else
    echo "❌ Failed to generate Swagger documentation"
    exit 1
fi
