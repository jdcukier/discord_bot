#!/bin/bash

# Build Docker image locally
set -e

echo "🐳 Building Discord Bot Docker image..."
docker build -t discord-bot:latest .

echo "✅ Docker build completed successfully!"
echo ""
echo "🚀 To run the container:"
echo "docker run -d --name discord-bot -p 8080:8080 discord-bot:latest"
