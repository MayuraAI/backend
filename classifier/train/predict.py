import torch
from transformers import AutoTokenizer, AutoModelForSequenceClassification
import json
import numpy as np

class PromptClassifier:
    def __init__(self, model_path="./best_model"):
        # Load model and tokenizer
        self.device = torch.device('mps' if torch.backends.mps.is_available() else 'cpu')
        self.tokenizer = AutoTokenizer.from_pretrained(model_path)
        self.model = AutoModelForSequenceClassification.from_pretrained(model_path).to(self.device)
        self.model.eval()
        
        # Load category mapping
        with open('../data/data.json', 'r') as f:
            data = json.load(f)
        categories = sorted(list(set(item['Category'] for item in data)))
        self.id_to_category = {idx: cat for idx, cat in enumerate(categories)}
    
    def predict(self, text):
        # Tokenize input
        inputs = self.tokenizer(
            text,
            truncation=True,
            padding=True,
            max_length=128,
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
        
        # Create results dictionary
        results = {
            self.id_to_category[idx]: float(prob)
            for idx, prob in enumerate(probabilities)
        }
        
        # Sort by probability
        results = dict(sorted(results.items(), key=lambda x: x[1], reverse=True))
        
        return results

def main():
    # Example usage
    classifier = PromptClassifier()
    
    # Example prompts
    example_prompts = [
        "Write a Python function to calculate the factorial of a number",
        "Summarize the main points of World War II",
        "What's the weather like today?",
    ]
    
    print("Making predictions for example prompts:")
    for prompt in example_prompts:
        print(f"\nPrompt: {prompt}")
        predictions = classifier.predict(prompt)
        print("\nPredicted probabilities:")
        for category, prob in predictions.items():
            print(f"{category}: {prob:.4f}")

if __name__ == "__main__":
    main() 