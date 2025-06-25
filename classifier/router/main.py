"""
FastAPI server for prompt classification and routing.
"""
from typing import Dict, Any, Optional
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from prometheus_client import Counter, Histogram, Gauge
from starlette_prometheus import PrometheusMiddleware, metrics

from router.prompt_router import PromptRouter

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
print("Router initialized at startup")

class PromptRequest(BaseModel):
    prompt: str
    request_type: Optional[str] = "free"  # "pro" or "free"
    params: Optional[Dict[str, Any]] = None

class PromptResponse(BaseModel):
    primary_model: str
    primary_model_display_name: str
    secondary_model: str
    secondary_model_display_name: str
    default_model: str
    default_model_display_name: str
    metadata: Dict[str, Any]

@app.post("/complete", response_model=PromptResponse)
async def route_prompt_endpoint(request: PromptRequest):
    """Route a prompt to the most appropriate models."""
    CONCURRENT_REQUESTS.inc()
    
    try:
        with LATENCY_HISTOGRAM.labels('/complete').time():
            print(f"Processing prompt routing request: {len(request.prompt)} chars, type: {request.request_type}")
            
            # Validate request_type
            if request.request_type not in ["pro", "free"]:
                raise HTTPException(status_code=400, detail="request_type must be 'pro' or 'free'")
            
            # Route the prompt
            result = await router.route_prompt(
                request.prompt, 
                request.request_type
            )
            primary_model = result['primary_model']
            secondary_model = result['secondary_model']
            default_model = result['default_model']
            metadata = result['metadata']
            
            # Update metrics
            MODEL_SELECTION_COUNTER.labels(primary_model).inc()
            CATEGORY_COUNTER.labels(metadata['predicted_category']).inc()
            CONFIDENCE_GAUGE.set(metadata['confidence'])
            REQUEST_COUNT.labels('/complete', 'success').inc()
            
            print(f"Routing completed: {primary_model} (primary), {secondary_model} (secondary), category: {metadata['predicted_category']}")
            
            return PromptResponse(
                primary_model=primary_model,
                primary_model_display_name=result['primary_model_display_name'],
                secondary_model=secondary_model,
                secondary_model_display_name=result['secondary_model_display_name'],
                default_model=default_model,
                default_model_display_name=result['default_model_display_name'],
                metadata=metadata
            )
            
    except HTTPException:
        # Re-raise HTTP exceptions
        raise
    except Exception as e:
        print(f"Error processing request: {type(e).__name__}: {str(e)}")
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