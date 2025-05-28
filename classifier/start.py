#!/usr/bin/env python3
import yaml
import os
import sys
import logging
import argparse
from pathlib import Path

# Add parent directory to Python path
current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from classifier.router.prompt_router import PromptRouter

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

def initialize_models(train: bool = False):
    """Initialize and validate models before server start."""
    try:
        logger.info("Initializing router and loading models...")
        router = PromptRouter()
        
        if train:
            logger.info("Training mode enabled - forcing model training...")
            router.classifier._load_or_train_models(force_train=True)
        
        # Validate model initialization
        if not router.classifier or not router.classifier.embedding_model:
            raise RuntimeError("Embedding model failed to initialize")
        
        # Test model with a sample prompt to ensure everything is loaded
        logger.info("Testing model initialization...")
        try:
            test_prompt = "This is a test prompt"
            category, probs = router.classifier.predict(test_prompt)
            logger.info(f"Test prediction successful - predicted category: {category}")
            return True
        except Exception as e:
            logger.error(f"Error during test prediction: {e}")
            return False
        
        return True
    except Exception as e:
        logger.error(f"Failed to initialize models: {e}")
        return False

def start_server():
    """Load models and start the server."""
    # Parse command line arguments
    parser = argparse.ArgumentParser(description='Start the classifier server')
    parser.add_argument('--train', action='store_true', help='Train the model using data.json')
    args = parser.parse_args()

    # Load config
    try:
        with open("classifier/config/config.yaml", 'r') as f:
            config = yaml.safe_load(f)
    except Exception as e:
        logger.error(f"Error loading config: {e}")
        sys.exit(1)

    # Initialize models first
    if not initialize_models(train=args.train):
        logger.error("Model initialization failed, not starting server")
        sys.exit(1)
    
    logger.info("Models initialized successfully, starting server...")

    # Get server config
    server_config = config.get('server', {})
    
    # Build gunicorn command with config values and improved logging
    cmd = [
        "gunicorn",
        "classifier.router.main:app",
        f"--workers={server_config.get('workers', 4)}",
        "--worker-class=uvicorn.workers.UvicornWorker",
        f"--threads={server_config.get('threads', 2)}",
        f"--bind={server_config.get('host', '0.0.0.0')}:{server_config.get('port', 8000)}",
        "--timeout=120",
        "--keep-alive=5",
        "--access-logfile=-",  # Log access to stdout
        "--error-logfile=-",   # Log errors to stdout
        "--capture-output",    # Capture stdout/stderr from workers
        "--enable-stdio-inheritance",  # Enable stdio inheritance for better logging
        f"--log-level={server_config.get('log_level', 'info').lower()}",
        "--logger-class=gunicorn.glogging.Logger"  # Use gunicorn's logger
    ]

    # Execute gunicorn
    logger.info(f"Starting server with command: {' '.join(cmd)}")
    os.execvp("gunicorn", cmd)

if __name__ == "__main__":
    start_server() 