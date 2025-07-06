# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install git to support go modules from private repos (if needed)
RUN apk add --no-cache git

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy application source
COPY . .

# Build Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

# Install CA certs for HTTPS requests
RUN apk --no-cache add ca-certificates

# Working directory for app
WORKDIR /root/

# Copy built binary
COPY --from=builder /app/main .

# Expose app port
EXPOSE 8081

# Set default environment variables (can be overridden by docker-compose or .env)
ENV GIN_MODE=release
ENV PORT=8081

# Start the service
CMD ["./main"]
