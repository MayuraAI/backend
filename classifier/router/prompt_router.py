"""
Prompt router that selects the best model based on classification results.
"""
import time
from typing import Dict, Tuple, Any, Optional
from pathlib import Path
import sys
from logging_utils import DailyLogger
import re

current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from router.model import PromptClassifier
# i = 0
class PromptRouter:
    _KEYWORDS = {
        'math': {'calculate', 'solve', 'equation', 'integral', 'derivative', r'\d+'},
        'code_generation': {'import ', 'def ', 'class ', 'function', 'return ', 'print(', 'console.log', 'var ', 'let ', 'const '},
        'translation': {'translate', 'in spanish', 'in french', 'into german', 'to english', 'traduce', 'übersetze'},
        'summarization': {'summarize', 'summary', 'condense', 'tl;dr'},
        'extraction': {'extract', 'entities', 'fields', 'find all', 'parse'},
        'classification': {'classify', 'what category', 'which class', 'label'},
        'problem_solving': {'troubleshoot', 'fix', 'issue', 'error', 'debug', 'problem', 'why won’t'},
        'reasoning': {'why', 'how', 'step by step', 'reasoning', 'think about'},
        'data_analysis': {'data', 'plot', 'chart', 'visualize', 'csv', 'dataset', 'statistics', 'mean', 'median'},
        'research': {'research', 'study', 'analyze', 'compare', 'investigate', 'literature'},
        'writing': {'write an essay', 'draft', 'compose', 'letter', 'email', 'blog post', 'article'},
        'creative': {'story', 'poem', 'joke', 'imagine', 'creative', 'generate a poem', 'write a song'},
        'roleplay': {'you are', 'roleplay', 'act as', 'character', 'in character'},
        'conversation': {'hi', 'hello', 'how are you', 'chat with me', 'tell me about yourself'}
    }

    # Build regex patterns from keyword sets
    _PATTERNS: Dict[str, re.Pattern] = {
        category: re.compile('|'.join(re.escape(k) for k in kws), re.IGNORECASE)
        for category, kws in _KEYWORDS.items()
    }

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
            DailyLogger().warning("No default model found in config")
        
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
                DailyLogger().info("Config file changed, reloading")
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
                # Pro users get access to only pro models
                if model_tier == 'pro':
                    filtered_models[model_name] = model_data
            elif request_type == 'free':
                # Free users get access to only free models
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
                'provider_model_name': model_data.get('provider_model_name', model_name),
                'is_thinking_model': model_data.get('is_thinking_model', False)
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
                'provider_model_name': scores['provider_model_name'],
                'is_thinking_model': scores['is_thinking_model']
            }
            
        return normalized_scores

    def _apply_simple_prompt_rules(self, prompt: str) -> Optional[Tuple[str, Dict[str, float]]]:
        """Apply heuristic rules for prompt classification."""
        # Check each category in priority order
        # Primary weight 0.9, secondary 0.1 to a closely related category where applicable
        checks = [
            ('math', {'math': 0.95, 'reasoning': 0.05}),
            ('code_generation', {'code_generation': 0.9, 'problem_solving': 0.1}),
            ('translation', {'translation': 0.9, 'writing': 0.1}),
            ('summarization', {'summarization': 0.9, 'writing': 0.1}),
            ('extraction', {'extraction': 0.9, 'classification': 0.1}),
            ('classification', {'classification': 0.9, 'reasoning': 0.1}),
            ('problem_solving', {'problem_solving': 0.9, 'reasoning': 0.1}),
            ('reasoning', {'reasoning': 0.95, 'problem_solving': 0.05}),
            ('data_analysis', {'data_analysis': 0.9, 'research': 0.1}),
            ('research', {'research': 0.85, 'writing': 0.15}),
            ('writing', {'writing': 0.9, 'creative': 0.1}),
            ('creative', {'creative': 0.9, 'writing': 0.1}),
            ('roleplay', {'roleplay': 0.9, 'conversation': 0.1}),
            ('conversation', {'conversation': 0.9, 'roleplay': 0.1})
        ]

        for category, weights in checks:
            pattern = self._PATTERNS[category]
            if pattern.search(prompt):
                return category, weights

        # No rule matched
        return None

    async def route_prompt(self, prompt: str, request_type: str = "free") -> Dict[str, Any]:
        """Route a prompt to the most appropriate models."""
        
        # Check if config needs reloading
        # self._check_config_reload()
        
        # Filter models by tier
        filtered_models = self._filter_models_by_tier(request_type)

        
        # # Handle empty filtered_models before cycling
        # if not filtered_models:
        #     print(f"Warning: No models available for request type: {request_type}")
        #     # Fallback to default model if available
        #     if self.default_model and self.default_model in self.model_scores:
        #         filtered_models = {self.default_model: self.model_scores[self.default_model]}
        #     else:
        #         # Use first available model as last resort
        #         filtered_models = {list(self.model_scores.keys())[0]: list(self.model_scores.values())[0]}

        # # TEMPORARY: Cycle through models for testing
        # global i
        # i += 1
        # i = i % len(filtered_models)
        
        # # Get secondary model index with proper cycling
        # secondary_i = (i + 1) % len(filtered_models)
        
        # return {
        #     'primary_model': list(filtered_models.keys())[i],
        #     'primary_model_display_name': filtered_models[list(filtered_models.keys())[i]]['display_name'],
        #     'secondary_model': list(filtered_models.keys())[secondary_i],
        #     'secondary_model_display_name': filtered_models[list(filtered_models.keys())[secondary_i]]['display_name'],
        #     'default_model': self.default_model,
        #     'default_model_display_name': self.model_display_name_map.get(self.default_model, self.default_model),
        #     'metadata': {
        #         'predicted_category': 'research',
        #         'confidence': 0.8,
        #         'category_probabilities': {'research': 0.8, 'writing': 0.2},
        #         'quality_weight': 0.6,
        #         'cost_weight': 0.4,
        #         'model_scores': {
        #             list(filtered_models.keys())[i]: {
        #                 'quality_score': 0.8,
        #                 'normalized_quality': 0.8,
        #                 'cost': 0.0,
        #                 'normalized_cost': 0.0,
        #                 'final_score': 0.8,
        #                 'tier': filtered_models[list(filtered_models.keys())[i]].get('tier', 'free'),
        #                 'provider': filtered_models[list(filtered_models.keys())[i]].get('provider', 'unknown'),
        #                 'display_name': filtered_models[list(filtered_models.keys())[i]].get('display_name', list(filtered_models.keys())[i]),
        #                 'provider_model_name': filtered_models[list(filtered_models.keys())[i]].get('provider_model_name', list(filtered_models.keys())[i]),
        #                 'is_thinking_model': filtered_models[list(filtered_models.keys())[i]].get('is_thinking_model', False),
        #             },
        #             list(filtered_models.keys())[secondary_i]: {
        #                 'quality_score': 0.8,
        #                 'normalized_quality': 0.8,
        #                 'cost': 0.0,
        #                 'normalized_cost': 0.0,
        #                 'final_score': 0.8,
        #                 'tier': filtered_models[list(filtered_models.keys())[secondary_i]].get('tier', 'free'),
        #                 'provider': filtered_models[list(filtered_models.keys())[secondary_i]].get('provider', 'unknown'),
        #                 'display_name': filtered_models[list(filtered_models.keys())[secondary_i]].get('display_name', list(filtered_models.keys())[secondary_i]),
        #                 'provider_model_name': filtered_models[list(filtered_models.keys())[secondary_i]].get('provider_model_name', list(filtered_models.keys())[secondary_i]),
        #                 'is_thinking_model': filtered_models[list(filtered_models.keys())[secondary_i]].get('is_thinking_model', False),
        #             },
        #             self.default_model: {
        #                 'quality_score': 0.8,
        #                 'normalized_quality': 0.8,
        #                 'cost': 0.0,
        #                 'normalized_cost': 0.0,
        #                 'final_score': 0.8,
        #                 'tier': self.model_scores.get(self.default_model, {}).get('tier', 'free'),
        #                 'provider': self.model_scores.get(self.default_model, {}).get('provider', 'unknown'),
        #                 'display_name': self.model_scores.get(self.default_model, {}).get('display_name', self.default_model),
        #                 'provider_model_name': self.model_scores.get(self.default_model, {}).get('provider_model_name', self.default_model),
        #                 'is_thinking_model': self.model_scores.get(self.default_model, {}).get('is_thinking_model', False),
        #             }
        #         },
        #         'classification_method': 'rule_based'
        #     }
        # }
        
        # # This code is now unreachable due to the early return above
        # # but keeping it for when the temporary code is removed
        if not filtered_models:
            DailyLogger().warning(f"No models available for request type: {request_type}")
            # Fallback to default model if available
            if self.default_model and self.default_model in self.model_scores:
                filtered_models = {self.default_model: self.model_scores[self.default_model]}
            else:
                # Use first available model as last resort
                filtered_models = {list(self.model_scores.keys())[0]: list(self.model_scores.values())[0]}
        
        # Try simple rules first
        # simple_result = self._apply_simple_prompt_rules(prompt)
        simple_result = None
        if simple_result:
            predicted_category, category_probs = simple_result
        else:
            # Use ML classification
            category_probs = await self.classifier.classify_prompt(prompt)
            print(f"Category probabilities: {category_probs}")
            # log category probs
            for category, prob in category_probs.items():
                DailyLogger().info(f"Category: {category}, Probability: {prob}")
            predicted_category = max(category_probs, key=category_probs.get)
        
        # Get dynamic weights based on task importance
        quality_weight, cost_weight = self._get_dynamic_weights(category_probs)
        
        # Calculate model scores
        model_scores = self._calculate_model_scores(category_probs, filtered_models)
        normalized_scores = self._normalize_scores(model_scores, quality_weight, cost_weight)

        # log every model scores in each category
        for model, scores in model_scores.items():
            DailyLogger().info(f"Model: {model}, Quality Score: {scores['quality_score']}, Cost: {scores['cost']}")
        
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
        
        DailyLogger().info(f"Routing completed - Category: {predicted_category}, Primary: {primary_model}, Secondary: {secondary_model} - Classification Method: {metadata['classification_method']}")
        
        return {
            'primary_model': primary_model,
            'primary_model_display_name': normalized_scores[primary_model]['display_name'],
            'secondary_model': secondary_model,
            'secondary_model_display_name': normalized_scores[secondary_model]['display_name'],
            'default_model': self.default_model,
            'default_model_display_name': self.model_display_name_map.get(self.default_model, self.default_model),
            'metadata': metadata
        }
