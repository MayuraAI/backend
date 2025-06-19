"""
Prompt router that selects the best model based on classification results.
"""
import time
from typing import Dict, Tuple, Any, Optional, List
from pathlib import Path
import sys

current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from router.model import PromptClassifier

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
        self.model_display_name_map = {}
        
        # Find default model
        self.default_model = None
        for model_name, model_data in self.model_scores.items():
            self.model_display_name_map[model_name] = model_data.get('display_name', model_name)
            if model_data.get('is_default', False):
                self.default_model = model_name
                break
        
        if not self.default_model:
            print("Warning: No default model found in config")
        
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
                print("Config file changed, reloading")
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
        
        return quality_weight, cost_weight

    def _filter_models_by_tier(self, request_type: str) -> Dict[str, Dict[str, Any]]:
        """Filter models based on request type (pro/free)."""
        filtered_models = {}
        
        for model_name, model_data in self.model_scores.items():
            model_tier = model_data.get('tier', 'free')
            
            if request_type == 'pro':
                # Pro users get access to both pro and free models
                if model_tier == 'pro':
                    filtered_models[model_name] = model_data
            elif request_type == 'free':
                # Free users only get free models
                if model_tier == 'free':
                    filtered_models[model_name] = model_data
        
        return filtered_models

    def _calculate_model_scores(self, category_probs: Dict[str, float], filtered_models: Dict[str, Dict[str, Any]]) -> Dict[str, Dict[str, float]]:
        """Calculate scores for each model based on category probabilities."""
        model_scores = {}
        
        for model_name, model_data in filtered_models.items():
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
                'cost': cost,
                'tier': model_data.get('tier', 'free'),
                'provider': model_data.get('provider', 'unknown'),
                'display_name': model_data.get('display_name', model_name),
                'provider_model_name': model_data.get('provider_model_name', model_name)
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
                'final_score': final_score,
                'tier': scores['tier'],
                'provider': scores['provider'],
                'display_name': scores['display_name'],
                'provider_model_name': scores['provider_model_name']
            }
            
        return normalized_scores

    def _apply_simple_prompt_rules(self, prompt: str) -> Optional[Tuple[str, Dict[str, float]]]:
        """Apply simple heuristic rules for certain prompt patterns."""
        prompt_lower = prompt.lower()
        
        # Math and calculation patterns
        if any(keyword in prompt_lower for keyword in ['calculate', 'solve', 'math', 'equation', '=', '+', '-', '*', '/', 'derivative', 'integral']):
            return 'math', {'math': 0.95, 'reasoning': 0.05}
        
        # Code generation patterns
        if any(keyword in prompt_lower for keyword in ['code', 'function', 'class', 'import', 'def ', 'return', 'print(', 'console.log', 'var ', 'let ', 'const ']):
            return 'code_generation', {'code_generation': 0.9, 'problem_solving': 0.1}
        
        # Research patterns
        if any(keyword in prompt_lower for keyword in ['research', 'study', 'analyze', 'compare', 'investigate', 'literature']):
            return 'research', {'research': 0.8, 'writing': 0.2}
        
        return None

    async def route_prompt(self, prompt: str, request_type: str = "free") -> Dict[str, Any]:
        """Route a prompt to the most appropriate models."""
        
        # Check if config needs reloading
        self._check_config_reload()
        
        # Filter models by tier
        filtered_models = self._filter_models_by_tier(request_type)
        
        if not filtered_models:
            print(f"Warning: No models available for request type: {request_type}")
            # Fallback to default model if available
            if self.default_model and self.default_model in self.model_scores:
                filtered_models = {self.default_model: self.model_scores[self.default_model]}
            else:
                # Use first available model as last resort
                filtered_models = {list(self.model_scores.keys())[0]: list(self.model_scores.values())[0]}
        
        # Try simple rules first
        simple_result = self._apply_simple_prompt_rules(prompt)
        if simple_result:
            predicted_category, category_probs = simple_result
        else:
            # Use ML classification
            category_probs = await self.classifier.classify_prompt(prompt)
            predicted_category = max(category_probs, key=category_probs.get)
        
        # Get dynamic weights based on task importance
        quality_weight, cost_weight = self._get_dynamic_weights(category_probs)
        
        # Calculate model scores
        model_scores = self._calculate_model_scores(category_probs, filtered_models)
        normalized_scores = self._normalize_scores(model_scores, quality_weight, cost_weight)
        
        # Sort models by final score (descending)
        sorted_models = sorted(
            normalized_scores.items(),
            key=lambda x: x[1]['final_score'],
            reverse=True
        )
        
        # Select top models
        primary_model = sorted_models[0][0] if sorted_models else self.default_model
        secondary_model = sorted_models[1][0] if len(sorted_models) > 1 else primary_model
        
        # Metadata
        metadata = {
            'predicted_category': predicted_category,
            'confidence': max(category_probs.values()),
            'category_probabilities': category_probs,
            'quality_weight': quality_weight,
            'cost_weight': cost_weight,
            'model_scores': normalized_scores,
            'classification_method': 'rule_based' if simple_result else 'ml_classification'
        }
        
        print(f"Routing completed - Category: {predicted_category}, Primary: {primary_model}, Secondary: {secondary_model}")
        
        return {
            'primary_model': primary_model,
            'primary_model_display_name': normalized_scores[primary_model]['display_name'],
            'secondary_model': secondary_model,
            'secondary_model_display_name': normalized_scores[secondary_model]['display_name'],
            'default_model': self.default_model,
            'default_model_display_name': self.model_display_name_map.get(self.default_model, self.default_model),
            'metadata': metadata
        }
