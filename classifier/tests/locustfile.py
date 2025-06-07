from locust import HttpUser, task, between
import random

class PromptUser(HttpUser):
    # Wait between 1 and 2 seconds between tasks
    wait_time = between(1, 2)

    prompts = [
        "hello",
        "write a function",
        "explain quantum physics",
        "how to make pasta",
        "what is machine learning",
        "write a python function to calculate the fibonacci sequence",
        "write a python function to calculate the factorial of a number",
        "write a python function to calculate the prime numbers up to a given number",
        "write a python function to calculate the square root of a number",
        "write a python function to calculate the cube root of a number",
        "write a python function to calculate the square of a number",
        "write a python function to calculate the cube of a number",
        "explain the concept of machine learning",
        "explain the concept of deep learning",
        "explain the concept of reinforcement learning",
        "explain the concept of natural language processing",
        "explain the concept of computer vision",
        "explain the concept of robotics",
        "explain the concept of artificial intelligence",
        "What is the capital of France?",
        "What is the capital of Germany?",
        "What is the capital of Italy?",
        "What is the capital of Spain?",
        "What is the capital of Portugal?",
        "What is the capital of Greece?",
        "What is the capital of Turkey?",
    ]

    def on_start(self):
        """Initialize the user session."""
        # Set default headers
        self.client.headers = {
            'Content-Type': 'application/json',
            'Accept': 'text/event-stream'
        }

    @task
    def test_prompt(self):
        """Test the prompt endpoint."""
        with self.client.post("/complete", 
                            json={"message": random.choice(self.prompts)},
                            stream=True,
                            catch_response=True) as response:
            # Read all chunks from the stream
            text = ""
            for chunk in response.iter_lines():
                if chunk:  # filter out keep-alive new lines
                    continue
            response.success() 