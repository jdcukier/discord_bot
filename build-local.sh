#!/bin/bash

# Build locally then create Docker image
set -e

echo "🏗️  Building Discord Bot locally for ARM64..."

# Check if we're on the right platform for cross-compilation
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo "✅ Linux detected - building natively for ARM64"
    GOOS=linux GOARCH=arm64 go build -o main ./cmd/...
elif [[ "$OSTYPE" == "darwin"* ]]; then
    echo "✅ macOS detected - cross-compiling for ARM64"
    GOOS=linux GOARCH=arm64 go build -o main ./cmd/...
else
    echo "❌ Unsupported OS for ARM64 compilation"
    exit 1
fi

echo "✅ Local build completed!"

echo "🐳 Building Docker image..."
docker build -f Dockerfile -t discord-bot:latest .

echo "🧹 Cleaning up local binary..."
rm main

echo "✅ Docker build completed successfully!"
echo ""
echo "🚀 To run the container:"
echo "docker run -d --name discord-bot -p 8080:8080 --env-file .env discord-bot:latest"
