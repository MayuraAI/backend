"""
Prompt router that selects the best model based on classification results.
"""
import logging
import time
from typing import Dict, Tuple, Any
from pathlib import Path
import sys

current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from classifier.router.model import PromptClassifier

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

class PromptRouter:
    def __init__(self, config_path: str = "classifier/config/config.yaml"):
        """Initialize the prompt router."""
        self.classifier = PromptClassifier(config_path)
        self.config_path = Path(config_path)
        self._load_config()
        
        # Monitor config file for changes
        self._last_config_check = time.time()
        self._config_check_interval = 60  # Check every minute
        
    def _load_config(self):
        """Load router configuration."""
        self.config = self.classifier.config
        self.quality_weight = self.config['weights']['quality']
        self.cost_weight = self.config['weights']['cost']
        self.model_scores = self.config['model_scores']['models']
        
    def _check_config_reload(self):
        """Check if config needs to be reloaded."""
        current_time = time.time()
        if current_time - self._last_config_check > self._config_check_interval:
            if self.config_path.stat().st_mtime > self._last_config_check:
                logger.info("Config file changed, reloading...")
                self._load_config()
            self._last_config_check = current_time

    def _calculate_model_scores(self, category_probs: Dict[str, float]) -> Dict[str, Dict[str, float]]:
        """Calculate scores for each model based on category probabilities."""
        model_scores = {}
        
        for model_name, model_data in self.model_scores.items():
            # Get base cost
            cost = model_data.get('cost_per_request', 0)
            
            # Calculate quality score weighted by category probabilities
            quality_score = sum(
                prob * model_data.get(category, 0)
                for category, prob in category_probs.items()
            )
            
            # Store scores
            model_scores[model_name] = {
                'quality_score': quality_score,
                'cost': cost
            }
            
        return model_scores

    def _normalize_scores(self, model_scores: Dict[str, Dict[str, float]]) -> Dict[str, Dict[str, float]]:
        """Normalize quality and cost scores to 0-1 range."""
        # Get min/max values
        quality_scores = [scores['quality_score'] for scores in model_scores.values()]
        costs = [scores['cost'] for scores in model_scores.values()]
        
        max_quality = max(quality_scores) if quality_scores else 1
        min_quality = min(quality_scores) if quality_scores else 0
        max_cost = max(costs) if costs else 1
        min_cost = min(costs) if costs else 0
        
        # Normalize scores
        normalized_scores = {}
        for model, scores in model_scores.items():
            # Normalize quality (higher is better)
            if max_quality == min_quality:
                norm_quality = 1.0
            else:
                norm_quality = (scores['quality_score'] - min_quality) / (max_quality - min_quality)
            
            # Normalize cost (lower is better)
            if max_cost == min_cost:
                norm_cost = 1.0
            else:
                norm_cost = 1 - ((scores['cost'] - min_cost) / (max_cost - min_cost))
            
            # Calculate final score
            final_score = (self.quality_weight * norm_quality) + (self.cost_weight * norm_cost)
            
            normalized_scores[model] = {
                'quality_score': scores['quality_score'],
                'normalized_quality': norm_quality,
                'cost': scores['cost'],
                'normalized_cost': norm_cost,
                'final_score': final_score
            }
            
        return normalized_scores

    async def route_prompt(self, prompt: str) -> Tuple[str, Dict[str, Any]]:
        """
        Route a prompt to the best model.
        
        Returns:
            Tuple of (best_model, metadata)
        """
        start_time = time.time()
        
        # Check if config needs reloading
        self._check_config_reload()
        
        # Get category probabilities
        category, category_probs = self.classifier.predict(prompt)
        
        # Calculate model scores
        model_scores = self._calculate_model_scores(category_probs)
        normalized_scores = self._normalize_scores(model_scores)
        
        # Find best model
        best_model = max(normalized_scores.items(), key=lambda x: x[1]['final_score'])[0]
        
        # Prepare metadata
        metadata = {
            'processing_time': time.time() - start_time,
            'predicted_category': category,
            'category_probabilities': category_probs,
            'model_scores': normalized_scores,
            'selected_model': best_model,
            'confidence': normalized_scores[best_model]['final_score']
        }
        
        return best_model, metadata
