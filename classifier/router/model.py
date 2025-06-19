"""
Prompt classifier using fine-tuned transformer model.
"""
import torch
from pathlib import Path
from typing import Dict, List, Optional, Tuple
import yaml
from transformers import AutoTokenizer, AutoModelForSequenceClassification
import numpy as np
import sys

current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

# Global model instances for sharing across workers
# Can be modified via config
MAX_MODEL_INSTANCES = 2
_model_instances = []

class PromptClassifier:
    def __new__(cls, config_path: str = "config/config.yaml"):
        """Round-robin pattern for multiple model instances."""
        global _model_instances
        if not _model_instances:
            print(f"Creating {MAX_MODEL_INSTANCES} model instances")
            for _ in range(MAX_MODEL_INSTANCES):
                instance = super(PromptClassifier, cls).__new__(cls)
                instance._initialized = False
                _model_instances.append(instance)
        
        # Round-robin selection of model instance
        cls._current_instance = (getattr(cls, '_current_instance', -1) + 1) % MAX_MODEL_INSTANCES
        return _model_instances[cls._current_instance]

    def __init__(self, config_path: str = "config/config.yaml"):
        """Initialize the classifier with config."""
        # Skip initialization if already done
        if getattr(self, '_initialized', False):
            return
            
        print("Initializing model instance")
        self.config = self._load_config(config_path)
        
        # Initialize model and tokenizer
        self._init_model()
        self._initialized = True
        print("Model initialization complete")

    def _load_config(self, config_path: str) -> dict:
        """Load configuration from YAML file."""
        with open(config_path, 'r') as f:
            return yaml.safe_load(f)

    def _init_model(self):
        
        model_path = Path(self.config['model']['save_dir']) / "best_model"
        
        if not model_path.exists():
            raise ValueError(f"Model not found at {model_path}. Please train the model first.")
        
        print(f"Loading model and tokenizer from {model_path}")
        
        # Load tokenizer and model
        self.tokenizer = AutoTokenizer.from_pretrained(str(model_path))
        self.model = AutoModelForSequenceClassification.from_pretrained(str(model_path))
        
        # Move model to appropriate device
        self.device = torch.device('mps' if torch.backends.mps.is_available() else 'cpu')
        self.model.to(self.device)
        self.model.eval()
        
        # Load label mapping from model config
        config = self.model.config
        self.id_to_label = config.id2label
        self.label_mapping = {v: int(k) for k, v in config.id2label.items()}
        
        print(f"Model loaded on {self.device} with {len(self.label_mapping)} labels")

    async def classify_prompt(self, text: str) -> Dict[str, float]:
        """
        Classify prompt and return probability distribution.
        
        Returns:
            Dictionary of category probabilities
        """
        # Tokenize input
        inputs = self.tokenizer(
            text,
            truncation=True,
            padding=True,
            max_length=512,
            return_tensors="pt"
        )
        
        # Move inputs to device
        inputs = {k: v.to(self.device) for k, v in inputs.items()}
        
        # Get predictions
        with torch.no_grad():
            outputs = self.model(**inputs)
            logits = outputs.logits
            probs = torch.nn.functional.softmax(logits, dim=-1)
        
        # Convert to numpy for easier handling
        probs = probs[0].cpu().numpy()
        
        # Create probability dictionary
        prob_dict = {
            self.id_to_label[i]: float(p)
            for i, p in enumerate(probs)
        }
        
        return prob_dict
