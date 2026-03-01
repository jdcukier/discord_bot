#!/bin/bash
set -e

# Check for .env file
if [ ! -f .env ]; then
  echo "❌ .env file not found. Copy .env.example to .env and fill in values."
  exit 1
fi

# Build the image
echo "🐳 Building Discord Bot Docker image..."
docker build -t discord-bot:latest .

# Stop and remove any existing container
docker stop discord-bot 2>/dev/null || true
docker rm discord-bot 2>/dev/null || true

echo "🚀 Starting Discord Bot..."
docker run --rm \
  --name discord-bot \
  --env-file .env \
  -p 8080:8080 \
  discord-bot:latest
