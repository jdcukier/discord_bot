# Multi-stage Dockerfile for Discord Bot
# Build-time variables
ARG APP_NAME=discordbot
ARG APP_USER=botuser
ARG APP_UID=1001

# Stage 1: Build the Go binary
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETPLATFORM
ARG BUILDPLATFORM

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary for target platform
RUN CGO_ENABLED=0 GOOS=${TARGETPLATFORM%/*} GOARCH=${TARGETPLATFORM#*/} \
    go build -a -installsuffix cgo -o main ./cmd

# Stage 2: Runtime container
FROM alpine:latest

# Import build-time variables
ARG APP_NAME
ARG APP_USER
ARG APP_UID

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata wget

# Create non-root user for security
RUN addgroup -g ${APP_UID} -S ${APP_USER} && \
    adduser -u ${APP_UID} -S ${APP_USER} -G ${APP_USER}

# Set working directory
WORKDIR /app

# Copy binary from builder stage and .env file
COPY --from=builder /app/main .
COPY .env .

# Set proper ownership
RUN chown ${APP_USER}:${APP_USER} /app/main /app/.env

# Switch to non-root user
USER ${APP_USER}

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./main"]
