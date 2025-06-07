"""
Prompt router that selects the best model based on classification results.
"""
import time
from typing import Dict, Tuple, Any, Optional
from pathlib import Path
import sys

current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from router.model import PromptClassifier
from router.logging_config import get_logger, log_performance

logger = get_logger(__name__)

class PromptRouter:
    def __init__(self, config_path: str = "config/config.yaml"):
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
        
        # Define task importance levels
        self.task_importance = {
            # High importance tasks - prioritize quality heavily
            'math': 'critical',
            'code_generation': 'critical', 
            'reasoning': 'critical',
            'problem_solving': 'critical',
            'research': 'critical',
            'data_analysis': 'critical',
            
            # Medium importance tasks - balanced approach
            'translation': 'medium',
            'writing': 'medium',
            'summarization': 'medium',
            'extraction': 'medium',
            'creative': 'medium',
            
            # Low importance tasks - prioritize cost
            'conversation': 'casual',
            'roleplay': 'casual',
            'classification': 'casual'
        }
        
    def _check_config_reload(self):
        """Check if config needs to be reloaded."""
        current_time = time.time()
        if current_time - self._last_config_check > self._config_check_interval:
            if self.config_path.stat().st_mtime > self._last_config_check:
                logger.info("Config file changed, reloading")
                self._load_config()
            self._last_config_check = current_time

    def _get_dynamic_weights(self, category_probs: Dict[str, float]) -> Tuple[float, float]:
        """Calculate dynamic quality/cost weights based on predicted task importance."""
        
        # Calculate weighted importance score
        importance_score = 0.0
        total_prob = 0.0
        
        for category, prob in category_probs.items():
            importance_level = self.task_importance.get(category, 'medium')
            
            # Convert importance to numeric score
            if importance_level == 'critical':
                category_importance = 1.0  # Heavily prioritize quality
            elif importance_level == 'medium':
                category_importance = 0.5  # Balanced approach
            else:  # casual
                category_importance = 0.0  # Heavily prioritize cost
            
            importance_score += prob * category_importance
            total_prob += prob
        
        # Normalize if needed
        if total_prob > 0:
            importance_score /= total_prob
        
        # Calculate dynamic weights based on importance
        if importance_score >= 0.7:  # Critical tasks
            quality_weight = 0.9
            cost_weight = 0.1
        elif importance_score >= 0.4:  # Medium importance tasks
            quality_weight = 0.6
            cost_weight = 0.4
        else:  # Casual tasks
            quality_weight = 0.2
            cost_weight = 0.8
        
        logger.debug("Dynamic weights calculated", extra_fields={
            'importance_score': round(importance_score, 3),
            'quality_weight': quality_weight,
            'cost_weight': cost_weight
        })
        return quality_weight, cost_weight

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

    def _normalize_scores(self, model_scores: Dict[str, Dict[str, float]], quality_weight: float, cost_weight: float) -> Dict[str, Dict[str, float]]:
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
            
            # Calculate final score using dynamic weights
            final_score = (quality_weight * norm_quality) + (cost_weight * norm_cost)
            
            normalized_scores[model] = {
                'quality_score': scores['quality_score'],
                'normalized_quality': norm_quality,
                'cost': scores['cost'],
                'normalized_cost': norm_cost,
                'final_score': final_score
            }
            
        return normalized_scores

    def _apply_simple_prompt_rules(self, prompt: str) -> Optional[Tuple[str, Dict[str, float]]]:
        """Apply simple rules for obvious prompt types to avoid misclassification."""
        
        prompt_lower = prompt.lower().strip()
        
        # Simple greetings and casual conversation starters
        simple_greetings = [
            'hello', 'hi', 'hey', 'good morning', 'good afternoon', 'good evening',
            'how are you', 'whats up', "what's up", 'howdy', 'greetings',
            'nice to meet you', 'pleased to meet you'
        ]
        
        # Check if it's a simple greeting (should be conversation)
        if any(greeting in prompt_lower for greeting in simple_greetings):
            return 'conversation', {'conversation': 0.95, 'roleplay': 0.05}
        
        # Simple math expressions (should be math)
        import re
        if re.match(r'^[0-9+\-*/().\s=x^]+$', prompt_lower):
            return 'math', {'math': 0.90, 'problem_solving': 0.10}
        
        # Code keywords (should be code_generation) - be more specific
        code_keywords = ['def ', 'class ', 'import ', 'function ', 'console.log', '#!/', 'return ', 'if __name__']
        if any(keyword in prompt_lower for keyword in code_keywords):
            return 'code_generation', {'code_generation': 0.85, 'problem_solving': 0.15}
        
        # More specific code patterns
        if ('print(' in prompt_lower and any(char in prompt_lower for char in ['(', ')', '"', "'"])):
            return 'code_generation', {'code_generation': 0.85, 'problem_solving': 0.15}
        
        return None

    @log_performance("route_prompt", 50.0)
    async def route_prompt(self, prompt: str) -> Tuple[str, Dict[str, Any]]:
        """
        Route a prompt to the best model.
        
        Returns:
            Tuple of (best_model, metadata)
        """
        start_time = time.time()
        
        # Check if config needs reloading
        self._check_config_reload()
        
        # First, try simple rules for obvious cases
        simple_result = self._apply_simple_prompt_rules(prompt)
        if simple_result:
            category, category_probs = simple_result
            logger.debug("Applied simple rule", extra_fields={
                'prompt_preview': prompt[:50],
                'predicted_category': category,
                'rule_type': 'simple'
            })
        else:
            # Get category probabilities from ML model
            category, category_probs = self.classifier.predict(prompt)
            logger.debug("ML classification completed", extra_fields={
                'predicted_category': category,
                'top_probability': max(category_probs.values()),
                'rule_type': 'ml'
            })
        
        # Get dynamic weights based on task importance
        quality_weight, cost_weight = self._get_dynamic_weights(category_probs)
        
        # Calculate model scores
        model_scores = self._calculate_model_scores(category_probs)
        normalized_scores = self._normalize_scores(model_scores, quality_weight, cost_weight)
        
        # Find best model
        best_model = max(normalized_scores.items(), key=lambda x: x[1]['final_score'])[0]
        
        # Prepare metadata
        metadata = {
            'processing_time': time.time() - start_time,
            'predicted_category': category,
            'category_probabilities': category_probs,
            'model_scores': normalized_scores,
            'selected_model': best_model,
            'confidence': normalized_scores[best_model]['final_score'],
            'dynamic_weights': {
                'quality_weight': quality_weight,
                'cost_weight': cost_weight
            }
        }
        
        logger.info("Prompt routing completed", extra_fields={
            'selected_model': best_model,
            'predicted_category': category,
            'confidence': round(metadata['confidence'], 3),
            'processing_time_ms': round(metadata['processing_time'] * 1000, 2)
        })
        
        return best_model, metadata
