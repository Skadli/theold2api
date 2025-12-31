# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod file
COPY go.mod ./

# Download dependencies (if any)
RUN go mod download 2>/dev/null || true

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o theold2api .

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
# Copy binary from builder
COPY --from=builder /app/theold2api .
COPY models.json .

# Create non-root user
RUN adduser -D -g '' appuser
USER appuser

# Environment variables
ENV PORT=8080
ENV API_KEY="meai123654789"
ENV UPSTREAM_URL="https://theoldllm.vercel.app/api/proxy?provider=p7"
ENV MAX_IDLE_CONNS=100
ENV MAX_CONNS_PER_HOST=100
ENV REQUEST_TIMEOUT=300s

# Proxy pool configuration
# Set PROXY_ENABLED=true to enable proxy pool
ENV PROXY_ENABLED=true
# Comma-separated list of proxy URLs
ENV PROXY_URLS="http://23.95.91.162:8081"
# Comma-separated list of proxy usernames (matching order with PROXY_URLS)
ENV PROXY_USERNAMES="proxy-a578c646"
# Comma-separated list of proxy passwords (matching order with PROXY_URLS)
ENV PROXY_PASSWORDS="7ZZ1xCQVG6sxSvWFraZsfbnA"
# Health check interval for proxies
ENV PROXY_HEALTH_CHECK=30s
# Number of retries before marking proxy as unhealthy
ENV PROXY_RETRY_COUNT=3

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the binary
ENTRYPOINT ["./theold2api"]
