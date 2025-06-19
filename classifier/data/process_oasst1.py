#!/usr/bin/env python3
"""
OpenAssistant OASST1 Dataset Processor
Processes prompts and scores them across multiple categories using Ollama local API
"""

import json
import csv
import os
import time
import requests
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Tuple
from dataclasses import dataclass
from pathlib import Path
import pandas as pd
from datasets import load_dataset
from tqdm import tqdm
import logging

# Configuration
OLLAMA_API_URL = "http://localhost:11434/api/generate"
MODEL_NAME = "gemma3:4B"  # Better model for classification tasks
BATCH_SIZE = 100
OUTPUT_FILE = "oasst1_scored_prompts.csv"
CHECKPOINT_FILE = "processing_checkpoint.json"
LOG_FILE = "processing.log"

# Categories from config.yaml
CATEGORIES = [
    "conversation", "classification", "roleplay", "data_analysis", 
    "translation", "problem_solving", "reasoning", "code_generation",
    "writing", "summarization", "math", "creative", "research", "extraction"
]

@dataclass
class ProcessingState:
    """Tracks processing state for resume capability"""
    total_prompts: int
    processed_count: int
    current_batch: int
    start_time: float
    failed_prompts: List[int] = None
    
    def __post_init__(self):
        if self.failed_prompts is None:
            self.failed_prompts = []

class OllamaScorer:
    """Handles communication with Ollama API for scoring prompts"""
    
    def __init__(self, model_name: str = MODEL_NAME):
        self.model_name = model_name
        self.session = requests.Session()
        
    def test_connection(self) -> bool:
        """Test if Ollama is running and model is available"""
        try:
            response = self.session.get(
                "http://localhost:11434/api/tags",
                timeout=10
            )
            if response.status_code == 200:
                models = response.json().get('models', [])
                model_names = [model['name'] for model in models]
                return any(self.model_name in name for name in model_names)
            return False
        except Exception as e:
            logging.error(f"Connection test failed: {e}")
            return False
    
    def score_prompt(self, prompt: str) -> Optional[str]:
        """Classify a single prompt into one category"""
        scoring_prompt = self._create_scoring_prompt(prompt)
        
        try:
            response = self.session.post(
                OLLAMA_API_URL,
                json={
                    "model": self.model_name,
                    "prompt": scoring_prompt,
                    "stream": False,
                    "options": {
                        "temperature": 0.1,  # Low temperature for consistent classification
                        "top_p": 0.9,
                        "num_predict": 50  # Much shorter response needed
                    }
                },
                timeout=30  # Shorter timeout since response is simpler
            )
            
            if response.status_code == 200:
                result = response.json()
                return self._parse_category(result.get('response', ''))
            else:
                logging.error(f"API error: {response.status_code} - {response.text}")
                return None
                
        except Exception as e:
            logging.error(f"Error classifying prompt: {e}")
            return None
    
    def _create_scoring_prompt(self, prompt: str) -> str:
        """Create the classification prompt for the model"""
        categories_str = ", ".join(CATEGORIES)
        
        return f"""You are an expert text classifier. Analyze the following prompt and choose the ONE category that best fits it.

CATEGORIES:
- conversation: Casual chat, greetings, personal discussion
- classification: Categorizing, labeling, identifying types
- roleplay: Acting as character, pretending, simulation
- data_analysis: Working with data, statistics, analysis
- translation: Converting between languages
- problem_solving: Finding solutions, troubleshooting
- reasoning: Logic, critical thinking, inference
- code_generation: Writing, debugging, or explaining code
- writing: Creative writing, essays, articles, stories
- summarization: Condensing, abstracting information
- math: Mathematical problems, calculations, formulas
- creative: Art, music, creative projects, brainstorming
- research: Information gathering, fact-finding
- extraction: Pulling specific information from text

PROMPT: "{prompt}"

Respond with ONLY the category name (no explanation, no other text):"""

    def _parse_category(self, response: str) -> Optional[str]:
        """Parse category from model response"""
        try:
            # Clean the response
            category = response.strip().lower()
            
            # Check if it's a valid category
            if category in CATEGORIES:
                return category
            
            # Try to find a valid category in the response
            for valid_category in CATEGORIES:
                if valid_category in category:
                    return valid_category
            
            # Default fallback
            logging.warning(f"Invalid category response: {response}, using 'conversation' as fallback")
            return "conversation"
            
        except Exception as e:
            logging.error(f"Error parsing category: {e}")
            return "conversation"

class DatasetProcessor:
    """Main processor for the OASST1 dataset"""
    
    def __init__(self, data_dir: str = "."):
        self.data_dir = Path(data_dir)
        self.scorer = OllamaScorer()
        self.output_file = self.data_dir / OUTPUT_FILE
        self.checkpoint_file = self.data_dir / CHECKPOINT_FILE
        self.log_file = self.data_dir / LOG_FILE
        
        # Setup logging
        logging.basicConfig(
            level=logging.INFO,
            format='%(asctime)s - %(levelname)s - %(message)s',
            handlers=[
                logging.FileHandler(self.log_file),
                logging.StreamHandler()
            ]
        )
        
    def load_dataset(self) -> List[Dict]:
        """Load and filter the OASST1 dataset"""
        logging.info("Loading OpenAssistant/oasst1 dataset...")
        
        try:
            dataset = load_dataset("OpenAssistant/oasst1", split="train")
            
            # Filter for English prompts only (role == 'prompter' and lang == 'en')
            prompts = []
            for item in dataset:
                if item.get('role') == 'prompter' and item.get('lang') == 'en':
                    prompts.append({
                        'text': item.get('text', '').strip(),
                    })
            
            logging.info(f"Loaded {len(prompts)} English prompts from dataset")
            return prompts
            
        except Exception as e:
            logging.error(f"Error loading dataset: {e}")
            raise
    
    def load_checkpoint(self) -> Optional[ProcessingState]:
        """Load processing state from checkpoint"""
        if self.checkpoint_file.exists():
            try:
                with open(self.checkpoint_file, 'r') as f:
                    data = json.load(f)
                    return ProcessingState(**data)
            except Exception as e:
                logging.error(f"Error loading checkpoint: {e}")
        return None
    
    def save_checkpoint(self, state: ProcessingState):
        """Save processing state to checkpoint"""
        try:
            with open(self.checkpoint_file, 'w') as f:
                json.dump({
                    'total_prompts': state.total_prompts,
                    'processed_count': state.processed_count,
                    'current_batch': state.current_batch,
                    'start_time': state.start_time,
                    'failed_prompts': state.failed_prompts
                }, f)
        except Exception as e:
            logging.error(f"Error saving checkpoint: {e}")
    
    def calculate_eta(self, state: ProcessingState) -> str:
        """Calculate estimated time remaining"""
        if state.processed_count == 0:
            return "Calculating..."
        
        elapsed = time.time() - state.start_time
        rate = state.processed_count / elapsed
        remaining = state.total_prompts - state.processed_count
        eta_seconds = remaining / rate if rate > 0 else 0
        
        eta_delta = timedelta(seconds=int(eta_seconds))
        return str(eta_delta)
    
    def process_batch(self, prompts_batch: List[Dict], batch_num: int) -> List[Dict]:
        """Process a batch of prompts"""
        results = []
        
        batch_progress = tqdm(
            prompts_batch, 
            desc=f"Batch {batch_num}", 
            leave=False
        )
        
        for i, prompt_data in enumerate(batch_progress):
            prompt_text = prompt_data['text']
            
            # Skip empty prompts
            if not prompt_text.strip():
                continue
            
            category = self.scorer.score_prompt(prompt_text)
            
            if category:
                result = {
                    'sno': (batch_num - 1) * BATCH_SIZE + i + 1,
                    'prompt': prompt_text,
                    'category': category
                }
                results.append(result)
            else:
                logging.warning(f"Failed to classify prompt {i} in batch {batch_num}")
        
        return results
    
    def write_results_to_csv(self, results: List[Dict], is_first_batch: bool = False):
        """Write results to CSV file"""
        if not results:
            return
        
        mode = 'w' if is_first_batch else 'a'
        write_header = is_first_batch
        
        with open(self.output_file, mode, newline='', encoding='utf-8') as f:
            fieldnames = ['sno', 'prompt', 'category']
            writer = csv.DictWriter(f, fieldnames=fieldnames)
            
            if write_header:
                writer.writeheader()
            
            writer.writerows(results)
    
    def run(self):
        """Main processing loop"""
        # Test Ollama connection
        if not self.scorer.test_connection():
            logging.error(f"Cannot connect to Ollama or model {MODEL_NAME} not found!")
            logging.error("Please ensure Ollama is running and the model is installed:")
            logging.error(f"  ollama pull {MODEL_NAME}")
            return
        
        logging.info(f"Connected to Ollama with model: {MODEL_NAME}")
        
        # Load dataset
        prompts = self.load_dataset()
        
        # Check for existing checkpoint
        state = self.load_checkpoint()
        
        if state:
            logging.info(f"Resuming from checkpoint: {state.processed_count}/{state.total_prompts} processed")
            start_batch = state.current_batch
        else:
            logging.info("Starting fresh processing")
            state = ProcessingState(
                total_prompts=len(prompts),
                processed_count=0,
                current_batch=1,
                start_time=time.time()
            )
            start_batch = 1
        
        # Process in batches
        total_batches = (len(prompts) + BATCH_SIZE - 1) // BATCH_SIZE
        
        # Main progress bar
        main_progress = tqdm(
            total=len(prompts),
            initial=state.processed_count,
            desc="Overall Progress",
            unit="prompts"
        )
        
        try:
            for batch_num in range(start_batch, total_batches + 1):
                start_idx = (batch_num - 1) * BATCH_SIZE
                end_idx = min(start_idx + BATCH_SIZE, len(prompts))
                batch_prompts = prompts[start_idx:end_idx]
                
                # Process batch
                results = self.process_batch(batch_prompts, batch_num)
                
                # Write results
                is_first_batch = (batch_num == 1 and state.processed_count == 0)
                self.write_results_to_csv(results, is_first_batch)
                
                # Update state
                state.processed_count += len(batch_prompts)
                state.current_batch = batch_num + 1
                
                # Update progress
                main_progress.update(len(batch_prompts))
                eta = self.calculate_eta(state)
                main_progress.set_postfix({
                    'ETA': eta,
                    'Batch': f"{batch_num}/{total_batches}"
                })
                
                # Save checkpoint
                self.save_checkpoint(state)
                
                # Brief pause to prevent overwhelming the API
                time.sleep(0.1)
        
        except KeyboardInterrupt:
            logging.info("Processing interrupted by user")
            self.save_checkpoint(state)
        except Exception as e:
            logging.error(f"Processing error: {e}")
            self.save_checkpoint(state)
            raise
        finally:
            main_progress.close()
        
        # Cleanup checkpoint on successful completion
        if state.processed_count >= len(prompts):
            if self.checkpoint_file.exists():
                self.checkpoint_file.unlink()
            logging.info(f"Processing completed! Results saved to {self.output_file}")
            logging.info(f"Total prompts processed: {state.processed_count}")

def main():
    """Main entry point"""
    print("OpenAssistant OASST1 Dataset Processor")
    print("=" * 50)
    print(f"Model: {MODEL_NAME}")
    print(f"Batch size: {BATCH_SIZE}")
    print(f"Categories: {len(CATEGORIES)}")
    print("Task: Single category classification")
    print(f"Output: {OUTPUT_FILE}")
    print()
    
    processor = DatasetProcessor()
    processor.run()

if __name__ == "__main__":
    main() 