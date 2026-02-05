import requests
import json

class APIClient:
    def __init__(self, base_url: str):
        self.base_url = base_url

    def fetch_data(self, endpoint: str) -> dict:
        try:
            response = requests.get(f"{self.base_url}/{endpoint}")
            response.raise_for_status()
            return response.json()
        # BUG: Bare Except
        # Catching all exceptions hides unexpected errors (e.g., NameError, KeyboardInterrupt)
        # and makes debugging extremely difficult.
        except:
            print("Something went wrong")
            return {}

    def send_data(self, endpoint: str, data: dict) -> bool:
        try:
            response = requests.post(f"{self.base_url}/{endpoint}", json=data)
            return response.status_code == 200
        except Exception as e:
            # Better but still too broad if we only expect Network/HTTP errors
            print(f"Error: {e}")
            return False

    # Dummy method
    def parse_headers(self, headers):
        return dict(headers)
