"""
Centralized logging configuration for the classifier service.
Provides structured logging with request correlation and consistent formatting.
"""
import logging
import json
import time
import uuid
from typing import Dict, Any, Optional
from contextvars import ContextVar
from functools import wraps

# Context variable for request correlation
request_id: ContextVar[Optional[str]] = ContextVar('request_id', default=None)


class StructuredFormatter(logging.Formatter):
    """Custom formatter for structured JSON logging."""
    
    def format(self, record: logging.LogRecord) -> str:
        """Format log record as structured JSON."""
        log_data = {
            'timestamp': time.strftime('%Y-%m-%d %H:%M:%S', time.localtime(record.created)),
            'level': record.levelname,
            'service': 'classifier',
            'logger': record.name,
            'message': record.getMessage(),
        }
        
        # Add request ID if available
        req_id = request_id.get()
        if req_id:
            log_data['request_id'] = req_id
        
        # Add extra fields if present
        if hasattr(record, 'extra_fields'):
            log_data.update(record.extra_fields)
        
        # Add exception info if present
        if record.exc_info:
            log_data['exception'] = self.formatException(record.exc_info)
        
        return json.dumps(log_data, ensure_ascii=False)


class RequestLogger:
    """Logger with request correlation support."""
    
    def __init__(self, name: str):
        self.logger = logging.getLogger(name)
    
    def _log(self, level: int, message: str, **kwargs):
        """Internal log method with structured data."""
        extra_fields = kwargs.pop('extra_fields', {})
        extra = {'extra_fields': extra_fields} if extra_fields else {}
        self.logger.log(level, message, extra=extra, **kwargs)
    
    def debug(self, message: str, **kwargs):
        self._log(logging.DEBUG, message, **kwargs)
    
    def info(self, message: str, **kwargs):
        self._log(logging.INFO, message, **kwargs)
    
    def warning(self, message: str, **kwargs):
        self._log(logging.WARNING, message, **kwargs)
    
    def error(self, message: str, **kwargs):
        self._log(logging.ERROR, message, **kwargs)
    
    def critical(self, message: str, **kwargs):
        self._log(logging.CRITICAL, message, **kwargs)


def setup_logging(log_level: str = "INFO", log_format: str = "structured") -> None:
    """Setup centralized logging configuration."""
    
    # Configure root logger
    root_logger = logging.getLogger()
    root_logger.setLevel(getattr(logging, log_level.upper()))
    
    # Remove existing handlers
    for handler in root_logger.handlers[:]:
        root_logger.removeHandler(handler)
    
    # Create console handler
    console_handler = logging.StreamHandler()
    
    if log_format == "structured":
        formatter = StructuredFormatter()
    else:
        formatter = logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - %(message)s'
        )
    
    console_handler.setFormatter(formatter)
    root_logger.addHandler(console_handler)
    
    # Reduce noise from external libraries
    logging.getLogger('uvicorn.access').setLevel(logging.WARNING)
    logging.getLogger('httpx').setLevel(logging.WARNING)
    logging.getLogger('httpcore').setLevel(logging.WARNING)


def generate_request_id() -> str:
    """Generate a unique request ID."""
    return str(uuid.uuid4())[:8]


def with_request_id(func):
    """Decorator to automatically generate and set request ID."""
    @wraps(func)
    async def async_wrapper(*args, **kwargs):
        req_id = generate_request_id()
        request_id.set(req_id)
        try:
            return await func(*args, **kwargs)
        finally:
            request_id.set(None)
    
    @wraps(func)
    def sync_wrapper(*args, **kwargs):
        req_id = generate_request_id()
        request_id.set(req_id)
        try:
            return func(*args, **kwargs)
        finally:
            request_id.set(None)
    
    # Return appropriate wrapper based on function type
    import asyncio
    if asyncio.iscoroutinefunction(func):
        return async_wrapper
    else:
        return sync_wrapper


def get_logger(name: str) -> RequestLogger:
    """Get a logger instance with request correlation support."""
    return RequestLogger(name)


# Performance monitoring decorator
def log_performance(operation_name: str, log_threshold_ms: float = 100.0):
    """Decorator to log performance metrics for operations."""
    def decorator(func):
        @wraps(func)
        async def async_wrapper(*args, **kwargs):
            logger = get_logger(f"performance.{operation_name}")
            start_time = time.time()
            
            try:
                result = await func(*args, **kwargs)
                duration_ms = (time.time() - start_time) * 1000
                
                if duration_ms > log_threshold_ms:
                    logger.info(
                        f"Operation '{operation_name}' completed",
                        extra_fields={
                            'operation': operation_name,
                            'duration_ms': round(duration_ms, 2),
                            'status': 'success'
                        }
                    )
                
                return result
            except Exception as e:
                duration_ms = (time.time() - start_time) * 1000
                logger.error(
                    f"Operation '{operation_name}' failed",
                    extra_fields={
                        'operation': operation_name,
                        'duration_ms': round(duration_ms, 2),
                        'status': 'error',
                        'error_type': type(e).__name__,
                        'error_message': str(e)
                    }
                )
                raise
        
        @wraps(func)
        def sync_wrapper(*args, **kwargs):
            logger = get_logger(f"performance.{operation_name}")
            start_time = time.time()
            
            try:
                result = func(*args, **kwargs)
                duration_ms = (time.time() - start_time) * 1000
                
                if duration_ms > log_threshold_ms:
                    logger.info(
                        f"Operation '{operation_name}' completed",
                        extra_fields={
                            'operation': operation_name,
                            'duration_ms': round(duration_ms, 2),
                            'status': 'success'
                        }
                    )
                
                return result
            except Exception as e:
                duration_ms = (time.time() - start_time) * 1000
                logger.error(
                    f"Operation '{operation_name}' failed",
                    extra_fields={
                        'operation': operation_name,
                        'duration_ms': round(duration_ms, 2),
                        'status': 'error',
                        'error_type': type(e).__name__,
                        'error_message': str(e)
                    }
                )
                raise
        
        # Return appropriate wrapper based on function type
        import asyncio
        if asyncio.iscoroutinefunction(func):
            return async_wrapper
        else:
            return sync_wrapper
    
    return decorator 