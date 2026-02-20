# Simple Dockerfile - just copy pre-built binary
FROM alpine:latest

# Install ca-certificates for HTTPS requests and tzdata
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1001 -S discordbot && \
    adduser -u 1001 -S discordbot -G discordbot

WORKDIR /app

# Copy the pre-built binary and .env file
COPY main .
COPY .env .

# Change ownership to non-root user
RUN chown discordbot:discordbot /app/main /app/.env

# Switch to non-root user
USER discordbot

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./main"]
