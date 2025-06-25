#!/usr/bin/env python3
import yaml
import os
import sys
import argparse
import asyncio
from pathlib import Path

# Add parent directory to Python path
current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from router.prompt_router import PromptRouter

async def initialize_models(train: bool = False):
    """Initialize and validate models before server start."""
    try:
        print("Initializing router and loading models")
        router = PromptRouter()
        
        if train:
            print("Training mode enabled - forcing model training")
            router.classifier._load_or_train_models(force_train=True)
        
        # Validate model initialization
        if not router.classifier:
            raise RuntimeError("Classifier failed to initialize")
        
        # Test model with a sample prompt to ensure everything is loaded
        print("Testing model initialization")
        try:
            test_prompt = "This is a test prompt"
            probs = await router.classifier.classify_prompt(test_prompt)
            predicted_category = max(probs, key=probs.get)
            print(f"Test prediction successful: {predicted_category}")
            return True
        except Exception as e:
            print(f"Error during test prediction: {type(e).__name__}: {str(e)}")
            return False
        
        return True
    except Exception as e:
        print(f"Failed to initialize models: {type(e).__name__}: {str(e)}")
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
        print(f"Error loading config: {type(e).__name__}: {str(e)}")
        sys.exit(1)

    # Initialize models first
    async def run_initialization():
        return await initialize_models(train=args.train)
    
    if not asyncio.run(run_initialization()):
        print("Model initialization failed, not starting server")
        sys.exit(1)

    print("Models initialized successfully, starting server")

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
    print(f"Starting server with gunicorn: {server_config.get('workers', 8)} workers on {server_config.get('host', '0.0.0.0')}:{server_config.get('port', 8000)}")
    os.execvp("gunicorn", cmd)

if __name__ == "__main__":
    start_server() 