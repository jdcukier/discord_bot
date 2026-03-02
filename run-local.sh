#!/bin/bash
set -e

# Check for .env file
if [ ! -f .env ]; then
  echo "❌ .env file not found. Copy .env.example to .env and fill in values."
  exit 1
fi

echo "🚀 Starting Discord Bot..."
docker compose up --build
