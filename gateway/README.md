# Backend Server

## Overview

This backend server provides intelligent model routing and streaming responses. It integrates with a classifier service to determine the best model for each request and can stream responses from Ollama when llama3.2 is selected.

## Features

- **Model Classification**: Automatically routes prompts to the most appropriate model
- **Streaming Responses**: Real-time streaming when using llama3.2 via Ollama
- **SSE Support**: Server-Sent Events for real-time communication
- **Docker Support**: Containerized classifier service with Gunicorn + Uvicorn workers

## Architecture

1. **Client Request** → Backend receives POST request with message
2. **Model Classification** → Classifier service determines best model
3. **Response Routing**:
   - If `llama3.2`: Stream from local Ollama API
   - If other model: Generate standard response
4. **SSE Streaming** → Real-time response delivery to client

## API Endpoints

### POST /complete

Processes a message and returns streaming response via SSE.

**Request Body:**
```json
{
  "message": "Your prompt here"
}
```

**Response:** Server-Sent Events stream

**SSE Message Format:**
```json
{
  "message": "Response chunk",
  "timestamp": "2025-05-28T14:19:27Z",
  "user_id": "user_id",
  "model": "llama3.2"
}
```

## Setup

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- Ollama (for llama3.2 streaming)
- Python 3.9+ (for classifier)

### Installation

1. **Start Classifier Service:**
   ```bash
   docker-compose up classifier
   ```

2. **Install Ollama and llama3.2:**
   ```bash
   # Install Ollama
   curl -fsSL https://ollama.ai/install.sh | sh
   
   # Pull llama3.2 model
   ollama pull llama3.2
   ```

3. **Build and Run Backend:**
   ```bash
   go build -o main cmd/main.go
   ./main
   ```

### Configuration

- **Classifier Service**: `http://localhost:8000/complete`
- **Ollama API**: `http://localhost:11434/api/generate`
- **Backend Server**: `http://localhost:8080/complete`

## Testing

Run the test script to verify streaming functionality:

```bash
python3 test_streaming.py
```

## Docker Configuration

The classifier runs with:
- **4 Gunicorn workers**
- **8 threads per worker**
- **UvicornWorker** for ASGI support
- **Resource limits**: 2 CPU cores, 2GB RAM

## Model Flow

```
User Message → Classifier → Model Selection
                ↓
    llama3.2 → Ollama API → Stream Response
    other → Standard Response → Single Message
```

## Error Handling

- Classifier service errors: Fallback to error response
- Ollama API errors: Error message via SSE
- Network timeouts: Graceful connection closure

## Monitoring

- Request logging with model selection details
- Processing time tracking
- Error rate monitoring
