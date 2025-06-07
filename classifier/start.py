#!/usr/bin/env python3
import yaml
import os
import sys
import argparse
from pathlib import Path

# Add parent directory to Python path
current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from router.prompt_router import PromptRouter
from router.logging_config import setup_logging, get_logger

# Setup structured logging
setup_logging(
    log_level=os.getenv("LOG_LEVEL", "INFO"),
    log_format=os.getenv("LOG_FORMAT", "structured")
)
logger = get_logger(__name__)

def initialize_models(train: bool = False):
    """Initialize and validate models before server start."""
    try:
        logger.info("Initializing router and loading models")
        router = PromptRouter()
        
        if train:
            logger.info("Training mode enabled - forcing model training")
            router.classifier._load_or_train_models(force_train=True)
        
        # Validate model initialization
        if not router.classifier or not router.classifier.embedding_model:
            raise RuntimeError("Embedding model failed to initialize")
        
        # Test model with a sample prompt to ensure everything is loaded
        logger.info("Testing model initialization")
        try:
            test_prompt = "This is a test prompt"
            category, probs = router.classifier.predict(test_prompt)
            logger.info("Test prediction successful", extra_fields={
                'predicted_category': category,
                'operation': 'model_initialization_test'
            })
            return True
        except Exception as e:
            logger.error("Error during test prediction", extra_fields={
                'operation': 'model_initialization_test',
                'error_type': type(e).__name__
            })
            return False
        
        return True
    except Exception as e:
        logger.error("Failed to initialize models", extra_fields={
            'operation': 'model_initialization',
            'error_type': type(e).__name__
        })
        return False

def start_server():
    """Load models and start the server."""
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='Start the classifier server')
    parser.add_argument('--train', action='store_true', help='Train the model using data.json')
    args = parser.parse_args()

    # Load config
    try:
        with open("config/config.yaml", 'r') as f:
            config = yaml.safe_load(f)
    except Exception as e:
        logger.error("Error loading config", extra_fields={'error_type': type(e).__name__})
        sys.exit(1)

    # Initialize models first
    if not initialize_models(train=args.train):
        logger.error("Model initialization failed, not starting server")
        sys.exit(1)

    logger.info("Models initialized successfully, starting server")

    # Get server config
    server_config = config.get('server', {})
    
    # Build gunicorn command with config values and improved logging
    cmd = [
        "gunicorn",
        "router.main:app",
        f"--workers={server_config.get('workers', 8)}",
        "--worker-class=uvicorn.workers.UvicornWorker",
        f"--threads={server_config.get('threads', 4)}",
        f"--bind={server_config.get('host', '0.0.0.0')}:{server_config.get('port', 8000)}",
        f"--timeout={server_config.get('timeout', 120)}",
        f"--keep-alive={server_config.get('keep_alive', 10)}",
        f"--max-requests={server_config.get('max_requests', 1000)}",
        f"--max-requests-jitter={server_config.get('max_requests_jitter', 50)}",
        f"--worker-connections={server_config.get('worker_connections', 1000)}",
        f"--backlog={server_config.get('backlog', 2048)}",
        "--access-logfile=-",  # Log access to stdout
        "--error-logfile=-",   # Log errors to stdout
        "--capture-output",    # Capture stdout/stderr from workers
        "--enable-stdio-inheritance",  # Enable stdio inheritance for better logging
        f"--log-level={server_config.get('log_level', 'info').lower()}",
        "--logger-class=gunicorn.glogging.Logger",  # Use gunicorn's logger
    ]
    
    # Add preload app if configured
    if server_config.get('preload_app', True):
        cmd.append("--preload")
    
    # Add worker restart settings for memory management
    cmd.extend([
        "--worker-tmp-dir=/dev/shm",  # Use shared memory for better performance
        "--graceful-timeout=30",      # Graceful shutdown timeout
        "--worker-class=uvicorn.workers.UvicornWorker",
    ])

    # Execute gunicorn
    logger.info("Starting server with gunicorn", extra_fields={
        'workers': server_config.get('workers', 8),
        'host': server_config.get('host', '0.0.0.0'),
        'port': server_config.get('port', 8000)
    })
    os.execvp("gunicorn", cmd)

if __name__ == "__main__":
    start_server() 