import json
import torch
from torch.utils.data import Dataset, DataLoader
from transformers import AutoTokenizer, AutoModelForSequenceClassification
from transformers import AdamW, get_linear_schedule_with_warmup
from sklearn.model_selection import train_test_split
from sklearn.metrics import classification_report
import numpy as np
from tqdm import tqdm
import pandas as pd
from pathlib import Path
from collections import Counter
import csv
from logging_utils import DailyLogger

# Constants
MODEL_NAME = "distilbert-base-uncased"
MAX_LENGTH = 512
BATCH_SIZE = 32
EPOCHS = 5
LEARNING_RATE = 3e-5
WARMUP_STEPS = 50
TRAIN_TEST_SPLIT = 0.2

class PromptDataset(Dataset):
    def __init__(self, texts, labels, tokenizer, max_length):
        self.encodings = tokenizer(texts, truncation=True, padding=True, max_length=max_length)
        self.labels = labels

    def __getitem__(self, idx):
        item = {key: torch.tensor(val[idx]) for key, val in self.encodings.items()}
        item['labels'] = torch.tensor(self.labels[idx])
        return item

    def __len__(self):
        return len(self.labels)

def load_data(data_path):
    """Load data from CSV file generated by the OASST1 processor"""
    texts = []
    categories = []
    
    try:
        with open(data_path, 'r', encoding='utf-8', newline='') as csvfile:
            # Try to detect the CSV format
            sample = csvfile.read(1024)
            csvfile.seek(0)
            sniffer = csv.Sniffer()
            delimiter = sniffer.sniff(sample).delimiter
            
            reader = csv.DictReader(csvfile, delimiter=delimiter)
            
            for row in reader:
                prompt = row.get('prompt', '').strip()
                category = row.get('category', '').strip()
                
                # Skip empty or invalid rows
                if prompt and category and category != 'nan':
                    texts.append(prompt)
                    categories.append(category)
    
    except Exception as e:
        DailyLogger().error(f"Error reading CSV file: {e}")
        raise
    
    if not texts:
        raise ValueError("No valid data found in CSV file")
    
    # Get unique categories and create label mapping
    unique_categories = sorted(list(set(categories)))
    category_to_id = {cat: idx for idx, cat in enumerate(unique_categories)}
    id_to_category = {idx: cat for cat, idx in category_to_id.items()}
    
    # Convert categories to numeric labels
    labels = [category_to_id[cat] for cat in categories]
    
    DailyLogger().info(f"Loaded {len(texts)} samples")
    DailyLogger().info(f"Categories found: {unique_categories}")
    
    return texts, labels, category_to_id, id_to_category

def train_epoch(model, dataloader, optimizer, scheduler, device):
    model.train()
    total_loss = 0
    progress_bar = tqdm(dataloader, desc='Training')
    
    for batch in progress_bar:
        optimizer.zero_grad()
        
        input_ids = batch['input_ids'].to(device)
        attention_mask = batch['attention_mask'].to(device)
        labels = batch['labels'].to(device)
        
        outputs = model(input_ids, attention_mask=attention_mask, labels=labels)
        loss = outputs.loss
        
        loss.backward()
        optimizer.step()
        scheduler.step()
        
        total_loss += loss.item()
        progress_bar.set_postfix({'loss': f'{loss.item():.4f}'})
    
    return total_loss / len(dataloader)

def evaluate(model, dataloader, device):
    model.eval()
    predictions = []
    true_labels = []
    
    with torch.no_grad():
        for batch in dataloader:
            input_ids = batch['input_ids'].to(device)
            attention_mask = batch['attention_mask'].to(device)
            labels = batch['labels']
            
            outputs = model(input_ids, attention_mask=attention_mask)
            logits = outputs.logits
            predictions.extend(torch.argmax(logits, dim=1).cpu().numpy())
            true_labels.extend(labels.numpy())
    
    return predictions, true_labels

def main():
    # Set device
    device = torch.device('mps' if torch.backends.mps.is_available() else 'cpu')
    DailyLogger().info(f"Using device: {device}")
    
    # Load data
    data_path = '../data/oasst1_scored_prompts.csv'
    if not Path(data_path).exists():
        DailyLogger().error(f"Error: Data file not found at {data_path}")
        DailyLogger().warning("Please run the OASST1 processing script first to generate the dataset.")
        return
    
    texts, labels, category_to_id, id_to_category = load_data(data_path)
    DailyLogger().info(f"Number of categories: {len(category_to_id)}")
    DailyLogger().info("Category distribution:")
    
    # Show category distribution
    category_counts = Counter([id_to_category[label] for label in labels])
    for cat, count in category_counts.most_common():
        DailyLogger().info(f"  {cat}: {count}")
    
    # Check if we have enough data for training
    if len(texts) < 100:
        DailyLogger().warning("Warning: Very small dataset. Consider processing more data.")
    
    # Split data
    train_texts, val_texts, train_labels, val_labels = train_test_split(
        texts, labels, test_size=TRAIN_TEST_SPLIT, random_state=42, stratify=labels
    )
    
    DailyLogger().info(f"Training samples: {len(train_texts)}")
    DailyLogger().info(f"Validation samples: {len(val_texts)}")
    
    # Load tokenizer and model
    tokenizer = AutoTokenizer.from_pretrained(MODEL_NAME)
    model = AutoModelForSequenceClassification.from_pretrained(
        MODEL_NAME, 
        num_labels=len(category_to_id),
        id2label={str(i): label for i, label in id_to_category.items()},
        label2id={label: i for i, label in id_to_category.items()}
    ).to(device)
    
    # Create datasets
    train_dataset = PromptDataset(train_texts, train_labels, tokenizer, MAX_LENGTH)
    val_dataset = PromptDataset(val_texts, val_labels, tokenizer, MAX_LENGTH)
    
    # Create dataloaders
    train_dataloader = DataLoader(train_dataset, batch_size=BATCH_SIZE, shuffle=True)
    val_dataloader = DataLoader(val_dataset, batch_size=BATCH_SIZE)
    
    # Setup training
    optimizer = AdamW(model.parameters(), lr=LEARNING_RATE)
    total_steps = len(train_dataloader) * EPOCHS
    scheduler = get_linear_schedule_with_warmup(
        optimizer,
        num_warmup_steps=WARMUP_STEPS,
        num_training_steps=total_steps
    )
    
    # Training loop
    best_accuracy = 0
    DailyLogger().info("Starting training...")
    
    for epoch in range(EPOCHS):
        DailyLogger().info(f"Epoch {epoch + 1}/{EPOCHS}")
        
        # Train
        avg_train_loss = train_epoch(model, train_dataloader, optimizer, scheduler, device)
        DailyLogger().info(f"Average training loss: {avg_train_loss:.4f}")
        
        # Evaluate
        predictions, true_labels = evaluate(model, val_dataloader, device)
        report = classification_report(
            true_labels, 
            predictions, 
            target_names=list(category_to_id.keys()),
            digits=4
        )
        DailyLogger().info("Validation Results:")
        DailyLogger().info(report)
        
        # Save best model
        accuracy = np.mean(np.array(predictions) == np.array(true_labels))
        if accuracy > best_accuracy:
            best_accuracy = accuracy
            DailyLogger().info(f"New best accuracy: {best_accuracy:.4f} - Saving model")
            save_dir = Path("./best_model")
            save_dir.mkdir(exist_ok=True)
            model.save_pretrained(save_dir)
            tokenizer.save_pretrained(save_dir)
            
            # Save category mappings
            mappings = {
                'category_to_id': category_to_id,
                'id_to_category': id_to_category
            }
            with open(save_dir / 'category_mappings.json', 'w') as f:
                json.dump(mappings, f, indent=2)
    
    DailyLogger().info("Training completed!")
    DailyLogger().info(f"Best validation accuracy: {best_accuracy:.4f}")
    DailyLogger().info(f"Model saved to: ./best_model/")

if __name__ == "__main__":
    main() 