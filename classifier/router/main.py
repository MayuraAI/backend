"""
FastAPI server for prompt classification and routing.
"""
import logging
from typing import Dict, Any, Optional
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from prometheus_client import Counter, Histogram, Gauge
from starlette_prometheus import PrometheusMiddleware, metrics

from classifier.router.prompt_router import PromptRouter

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# Initialize FastAPI app
app = FastAPI(
    title="Prompt Router API",
    description="API for routing prompts to the most appropriate model",
    version="1.0.0"
)

# Add CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Add Prometheus middleware
app.add_middleware(PrometheusMiddleware)
app.add_route("/metrics", metrics)

# Define metrics
REQUEST_COUNT = Counter(
    'prompt_router_requests_total',
    'Total number of requests processed',
    ['endpoint', 'status']
)

LATENCY_HISTOGRAM = Histogram(
    'prompt_router_request_duration_seconds',
    'Request duration in seconds',
    ['endpoint']
)

MODEL_SELECTION_COUNTER = Counter(
    'prompt_router_model_selections_total',
    'Number of times each model was selected',
    ['model']
)

CATEGORY_COUNTER = Counter(
    'prompt_router_category_predictions_total',
    'Number of times each category was predicted',
    ['category']
)

CONFIDENCE_GAUGE = Gauge(
    'prompt_router_confidence_score',
    'Confidence score of the last prediction'
)

CONCURRENT_REQUESTS = Gauge(
    'prompt_router_concurrent_requests',
    'Number of concurrent requests being processed'
)

# Initialize router at startup
router = PromptRouter()
logger.info("Router initialized at startup")

class PromptRequest(BaseModel):
    prompt: str
    params: Optional[Dict[str, Any]] = None

class PromptResponse(BaseModel):
    model: str
    metadata: Dict[str, Any]

@app.get("/health")
async def health_check():
    """Health check endpoint."""
    try:
        # Test the router with a simple prompt
        router.route_prompt("test prompt")
        return {"status": "healthy"}
    except Exception as e:
        logger.error(f"Health check failed: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/complete", response_model=PromptResponse)
async def route_prompt(request: PromptRequest):
    """Route a prompt to the most appropriate model."""
    CONCURRENT_REQUESTS.inc()
    
    try:
        with LATENCY_HISTOGRAM.labels('/complete').time():
            # Route the prompt
            model, metadata = await router.route_prompt(request.prompt)
            
            # Update metrics
            MODEL_SELECTION_COUNTER.labels(model).inc()
            CATEGORY_COUNTER.labels(metadata['predicted_category']).inc()
            CONFIDENCE_GAUGE.set(metadata['confidence'])
            REQUEST_COUNT.labels('/complete', 'success').inc()
            
            return PromptResponse(
                model=model,
                metadata=metadata
            )
            
    except Exception as e:
        logger.error(f"Error processing request: {e}")
        REQUEST_COUNT.labels('/complete', 'error').inc()
        raise HTTPException(status_code=500, detail="Internal server error")
    
    finally:
        CONCURRENT_REQUESTS.dec()

if __name__ == "__main__":
    import uvicorn
    import yaml
    
    # Load config
    with open("config/config.yaml") as f:
        config = yaml.safe_load(f)
    
    # Start server
    uvicorn.run(
        "main:app",
        host=config['server']['host'],
        port=config['server']['port'],
        workers=config['server']['workers'],
        threads=config['server']['threads'],
        log_level=config['server']['log_level'].lower()
    ) 