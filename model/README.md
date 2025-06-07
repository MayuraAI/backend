# Prompt Classification with TinyBERT

This directory contains code for fine-tuning TinyBERT for prompt classification. The model is optimized for CPU usage and requires only about 1GB of RAM.

## Features

- Fine-tunes TinyBERT for multi-class prompt classification
- Optimized for CPU and Apple M-series chips (using MPS)
- Low memory footprint (~85MB model size)
- Returns probability scores for all categories
- Includes detailed training metrics and evaluation

## Setup

1. Install dependencies:
```bash
pip install -r requirements.txt
```

2. Train the model:
```bash
python train.py
```
This will:
- Load data from `../classifier/data/data.json`
- Fine-tune TinyBERT on the data
- Save the best model to `./best_model/`
- Print training metrics and validation results

3. Make predictions:
```bash
python predict.py
```
This includes example usage of the trained model.

## Model Details

- Base model: `prajjwal1/bert-tiny` (4.4M parameters)
- Max sequence length: 128 tokens
- Training:
  - Batch size: 32
  - Learning rate: 2e-5
  - Epochs: 5
  - Train/Val split: 80/20

## Performance Metrics

The training script will output:
- Per-epoch training loss
- Validation metrics:
  - Accuracy
  - Precision
  - Recall
  - F1-score
  - Support (per category)

## Using the Model

```python
from predict import PromptClassifier

# Initialize classifier
classifier = PromptClassifier(model_path="./best_model")

# Make prediction
text = "Write a Python function to calculate factorial"
predictions = classifier.predict(text)

# Print predictions
for category, probability in predictions.items():
    print(f"{category}: {probability:.4f}")
```

## Memory Usage

- Model size: ~85MB
- Runtime memory: ~1GB
- Inference time: ~100ms per prediction on CPU 