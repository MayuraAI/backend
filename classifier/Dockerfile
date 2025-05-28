# Use slim Python image
FROM python:3.9-slim

# Set working directory
WORKDIR /app

# Set environment variables
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONPATH=/app \
    WORKERS=2 \
    THREADS=4

# Install system dependencies
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        gcc \
    && rm -rf /var/lib/apt/lists/*

# Copy requirements first to leverage Docker cache
COPY requirements.txt .

# Install Python dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY . .

# Create necessary directories
RUN mkdir -p classifier/models

# Expose port (default, actual port will be from config)
EXPOSE 8000

# Set default command to use Gunicorn with Uvicorn workers
CMD gunicorn classifier.router.main:app --workers $WORKERS --worker-class uvicorn.workers.UvicornWorker --threads $THREADS --bind 0.0.0.0:8000 