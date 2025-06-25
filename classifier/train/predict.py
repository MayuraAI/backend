import torch
from transformers import AutoTokenizer, AutoModelForSequenceClassification
import json
import numpy as np
from pathlib import Path

class PromptClassifier:
    def __init__(self, model_path="./best_model"):
        # Load model and tokenizer
        self.device = torch.device('mps' if torch.backends.mps.is_available() else 'cpu')
        self.model_path = Path(model_path)
        
        self.tokenizer = AutoTokenizer.from_pretrained(self.model_path)
        self.model = AutoModelForSequenceClassification.from_pretrained(self.model_path).to(self.device)
        self.model.eval()
        
        # Load category mapping from the saved mappings
        mappings_file = self.model_path / 'category_mappings.json'
        if mappings_file.exists():
            with open(mappings_file, 'r') as f:
                mappings = json.load(f)
                self.id_to_category = {int(k): v for k, v in mappings['id_to_category'].items()}
                self.category_to_id = mappings['category_to_id']
        else:
            # Fallback to old method if mappings file doesn't exist
            print("Warning: category_mappings.json not found, using fallback method")
            with open('../data/data.json', 'r') as f:
                data = json.load(f)
            categories = sorted(list(set(item['Category'] for item in data)))
            self.id_to_category = {idx: cat for idx, cat in enumerate(categories)}
            self.category_to_id = {cat: idx for idx, cat in enumerate(categories)}
    
    def predict(self, text, return_single_category=False):
        # Tokenize input
        inputs = self.tokenizer(
            text,
            truncation=True,
            padding=True,
            max_length=512,  # Updated to match training
            return_tensors="pt"
        )
        
        # Move inputs to device
        inputs = {k: v.to(self.device) for k, v in inputs.items()}
        
        # Get predictions
        with torch.no_grad():
            outputs = self.model(**inputs)
            probabilities = torch.nn.functional.softmax(outputs.logits, dim=1)
        
        # Convert to numpy for easier handling
        probabilities = probabilities[0].cpu().numpy()
        
        if return_single_category:
            # Return just the best category (like the new dataset format)
            best_idx = np.argmax(probabilities)
            return self.id_to_category[best_idx]
        
        # Create results dictionary with all probabilities
        results = {
            self.id_to_category[idx]: float(prob)
            for idx, prob in enumerate(probabilities)
        }
        
        # Sort by probability
        results = dict(sorted(results.items(), key=lambda x: x[1], reverse=True))
        
        return results
    
    def predict_batch(self, texts, return_single_category=False):
        """Predict categories for multiple texts"""
        results = []
        for text in texts:
            result = self.predict(text, return_single_category=return_single_category)
            results.append(result)
        return results

def main():
    # Example usage
    classifier = PromptClassifier()
    
    # Example prompts
    example_prompts = [
        "Write a Python function to calculate the factorial of a number",
        "Summarize the main points of World War II",
        "What's the weather like today?",
        "Can you help me analyze this dataset?",
        "Write a creative story about a dragon",
        "Solve this math problem: 2x + 5 = 15",
        "Translate 'hello world' to Spanish",
        "Act like you're a pirate captain"
    ]
    
    print("Making predictions for example prompts:")
    print("\n" + "="*60)
    
    for prompt in example_prompts:
        print(f"\nPrompt: {prompt}")
        
        # Get single category prediction (like new dataset format)
        single_category = classifier.predict(prompt, return_single_category=True)
        print(f"Best Category: {single_category}")
        
        # Get all probabilities
        predictions = classifier.predict(prompt)
        print("\nTop 3 predictions:")
        for i, (category, prob) in enumerate(list(predictions.items())[:3]):
            print(f"  {i+1}. {category}: {prob:.4f}")
        
        print("-" * 40)

if __name__ == "__main__":
    main() 