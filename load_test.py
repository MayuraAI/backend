from locust import HttpUser, task, between

class APITester(HttpUser):
    wait_time = between(0.1, 1.0)  # Random wait time between tasks

    @task
    def send_prompt(self):
        payload = {
            "prompt": "write a python program to reverse a string"
        }
        headers = {'Content-Type': 'application/json'}
        self.client.post("/complete", json=payload, headers=headers)
