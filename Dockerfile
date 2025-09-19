# Multi-stage build for SSH Tunnel Manager
# AI Generated Dockerfile

# Build stage
FROM golang:1.25.0-alpine AS builder

# Build arguments
ARG VERSION=dev
ARG BUILD_TIME
ARG COMMIT

# Install git and ca-certificates for dependency fetching
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with version info
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.Commit=${COMMIT}" \
    -o ssh-tunnel-manager .

# Final stage - minimal runtime image
FROM alpine:latest

# Install ca-certificates for SSL/TLS
RUN apk --no-cache add ca-certificates openssh-client

# Create non-root user
RUN adduser -D -s /bin/sh appuser

# Set working directory
WORKDIR /home/appuser

# Copy binary from builder stage
COPY --from=builder /app/ssh-tunnel-manager .

# Copy documentation
COPY --from=builder /app/README*.md ./
COPY --from=builder /app/CLAUDE.md ./

# Change ownership to non-root user
RUN chown -R appuser:appuser /home/appuser

# Switch to non-root user
USER appuser

# Create config directory
RUN mkdir -p .ssh-tunnel-manager

# Add labels
LABEL maintainer="AI Generated" \
      description="SSH Tunnel Manager - TUI tool for managing SSH tunnels" \
      version="${VERSION}" \
      ai.generated="true" \
      ai.model="Claude AI"

# Expose common tunnel ports (can be overridden)
EXPOSE 1080 3306 5432 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ./ssh-tunnel-manager --version || exit 1

# Default command
ENTRYPOINT ["./ssh-tunnel-manager"]
CMD ["--help"]