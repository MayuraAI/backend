import logging
import os
from logging.handlers import TimedRotatingFileHandler
from datetime import datetime
from threading import Lock

class DailyLogger:
    _instance = None
    _lock = Lock()

    def __new__(cls):
        if not cls._instance:
            with cls._lock:
                if not cls._instance:
                    cls._instance = super(DailyLogger, cls).__new__(cls)
                    cls._instance._setup_logger()
        return cls._instance

    def _setup_logger(self):
        os.makedirs('logs', exist_ok=True)
        log_filename = f"logs/log-{datetime.now().strftime('%Y-%m-%d')}.log"
        self.logger = logging.getLogger("DailyLogger")
        self.logger.setLevel(logging.INFO)
        # Avoid adding multiple handlers in case of multiple imports
        if not self.logger.handlers:
            handler = TimedRotatingFileHandler(log_filename, when='midnight', interval=1, backupCount=7, encoding='utf-8')
            formatter = logging.Formatter('%(asctime)s [%(levelname)s] %(name)s: %(message)s')
            handler.setFormatter(formatter)
            handler.suffix = "%Y-%m-%d"
            self.logger.addHandler(handler)

    def info(self, msg, *args, **kwargs):
        self.logger.info(msg, *args, **kwargs)

    def warning(self, msg, *args, **kwargs):
        self.logger.warning(msg, *args, **kwargs)

    def error(self, msg, *args, **kwargs):
        self.logger.error(msg, *args, **kwargs)

    def debug(self, msg, *args, **kwargs):
        self.logger.debug(msg, *args, **kwargs)

    def exception(self, msg, *args, **kwargs):
        self.logger.exception(msg, *args, **kwargs) 