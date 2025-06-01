"""
Prompt classifier using sentence transformers for embeddings.
"""
import numpy as np
import json
from pathlib import Path
from typing import Dict, List, Optional, Tuple
import yaml
from sentence_transformers import SentenceTransformer
from sklearn.linear_model import LogisticRegression
from sklearn.preprocessing import LabelEncoder
import sys

current_dir = Path(__file__).resolve().parent
parent_dir = current_dir.parent
if str(parent_dir) not in sys.path:
    sys.path.append(str(parent_dir))

from classifier.router.logging_config import get_logger, log_performance

logger = get_logger(__name__)

# Global model instance for sharing across workers
_model_instance = None

class PromptClassifier:
    def __new__(cls, config_path: str = "classifier/config/config.yaml"):
        """Singleton pattern to ensure only one model instance is created."""
        global _model_instance
        if _model_instance is None:
            logger.info("Creating new model instance")
            _model_instance = super(PromptClassifier, cls).__new__(cls)
            _model_instance._initialized = False
        return _model_instance

    def __init__(self, config_path: str = "classifier/config/config.yaml"):
        """Initialize the classifier with config."""
        # Skip initialization if already done
        if getattr(self, '_initialized', False):
            return
            
        logger.info("Initializing model")
        self.config = self._load_config(config_path)
        
        # Initialize models
        self._init_embedding_model()
        self.centroids: Dict[str, np.ndarray] = {}
        self.classifier: Optional[LogisticRegression] = None
        self.label_encoder: Optional[LabelEncoder] = None
        
        # Load or train models
        self._load_or_train_models()
        
        self._initialized = True
        logger.info("Model initialization complete")

    def _load_config(self, config_path: str) -> dict:
        """Load configuration from YAML file."""
        with open(config_path, 'r') as f:
            return yaml.safe_load(f)

    def _init_embedding_model(self):
        """Initialize the sentence transformer model."""
        model_name = self.config['model']['name']
        logger.info("Loading embedding model", extra_fields={'model_name': model_name})
        self.embedding_model = SentenceTransformer(model_name, device="cpu")

    def _get_embeddings(self, texts: List[str]) -> np.ndarray:
        """Get embeddings for input texts using sentence transformers."""
        return self.embedding_model.encode(texts, convert_to_numpy=True, show_progress_bar=False)

    def _load_or_train_models(self, force_train: bool = False):
        """Load pre-trained models or train new ones."""
        save_dir = Path(self.config['model']['save_dir'])
        centroids_path = save_dir / "centroids.npy"
        classifier_path = save_dir / "classifier.joblib"
        
        if not force_train:
            try:
                self._load_models(centroids_path, classifier_path)
                logger.info("Successfully loaded pre-trained models")
                return
            except (FileNotFoundError, ValueError) as e:
                logger.warning("Could not load pre-trained models", extra_fields={'error_type': type(e).__name__})
        
        logger.info("Training new models")
        self._train_from_data()

    def _load_models(self, centroids_path: Path, classifier_path: Path):
        """Load pre-trained models from disk."""
        if centroids_path.exists():
            self.centroids = np.load(str(centroids_path), allow_pickle=True).item()
        
        if classifier_path.exists():
            from joblib import load
            models = load(classifier_path)
            self.classifier = models['classifier']
            self.label_encoder = models['label_encoder']

    def _train_from_data(self):
        """Train models using data from data.json."""
        data_path = Path("classifier/data/data.json")
        if not data_path.exists():
            raise FileNotFoundError("Training data file not found at data/data.json")
        
        logger.info("Loading training data")
        with open(data_path, 'r') as f:
            data = json.load(f)
        
        texts = [item["Prompt"] for item in data]
        labels = [item["Category"] for item in data]
        
        logger.info("Training on examples", extra_fields={'example_count': len(texts)})
        self.train(texts, labels)

    def train(self, texts: List[str], labels: List[str]):
        """Train the classifier on new data."""
        if not texts or not labels:
            logger.warning("No training data provided")
            return

        # Get embeddings
        logger.info("Generating embeddings")
        embeddings = self._get_embeddings(texts)
        
        # Train label encoder
        if not self.label_encoder:
            self.label_encoder = LabelEncoder()
        
        encoded_labels = self.label_encoder.fit_transform(labels)
        
        # Train classifier
        logger.info("Training classifier")
        if not self.classifier:
            self.classifier = LogisticRegression(multi_class='ovr', max_iter=1000)
        
        self.classifier.fit(embeddings, encoded_labels)
        
        # Calculate centroids
        logger.info("Calculating centroids")
        unique_labels = self.label_encoder.classes_
        for label in unique_labels:
            mask = np.array(labels) == label
            label_embeddings = embeddings[mask]
            self.centroids[label] = np.mean(label_embeddings, axis=0)
        
        self._save_models()

    def _save_models(self):
        """Save trained models to disk."""
        save_dir = Path(self.config['model']['save_dir'])
        save_dir.mkdir(parents=True, exist_ok=True)
        
        # Save centroids
        np.save(str(save_dir / "centroids.npy"), self.centroids)
        
        # Save classifier and label encoder
        from joblib import dump
        models = {
            'classifier': self.classifier,
            'label_encoder': self.label_encoder
        }
        dump(models, str(save_dir / "classifier.joblib"))
        
        logger.info("Models saved", extra_fields={'save_dir': str(save_dir)})

    @log_performance("predict", 20.0)
    def predict(self, text: str) -> Tuple[str, Dict[str, float]]:
        """
        Predict category for input text.
        
        Returns:
            Tuple of (predicted_category, probability_dict)
        """
        if not self.classifier:
            raise ValueError("Models not initialized. Please train the classifier first.")
        
        if not self.label_encoder:
            raise ValueError("Label encoder not initialized. Please get the encoder first.")
            
        # Get embedding
        embedding = self._get_embeddings([text])[0]
        
        # Get probabilities
        probs = self.classifier.predict_proba([embedding])[0]
        
        # Create probability dictionary
        prob_dict = {
            cat: float(prob)
            for cat, prob in zip(self.label_encoder.classes_, probs)
        }
        
        # Get prediction
        predicted_idx = probs.argmax()
        predicted_category = self.label_encoder.classes_[predicted_idx]
        
        return predicted_category, prob_dict
