import logging
import os
from logging.handlers import TimedRotatingFileHandler
from datetime import datetime

# Ensure logs directory exists
os.makedirs('logs', exist_ok=True)

log_filename = f"logs/log-{datetime.now().strftime('%Y-%m-%d')}.log"

# Set up root logger with daily rotation
handler = TimedRotatingFileHandler(log_filename, when='midnight', interval=1, backupCount=7, encoding='utf-8')
formatter = logging.Formatter('%(asctime)s [%(levelname)s] %(name)s: %(message)s')
handler.setFormatter(formatter)
handler.suffix = "%Y-%m-%d"

root_logger = logging.getLogger()
root_logger.setLevel(logging.INFO)
if not root_logger.handlers:
    root_logger.addHandler(handler)
