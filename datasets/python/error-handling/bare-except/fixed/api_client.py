import requests
import json
from requests.exceptions import RequestException

class APIClient:
    def __init__(self, base_url: str):
        self.base_url = base_url

    def fetch_data(self, endpoint: str) -> dict:
        try:
            response = requests.get(f"{self.base_url}/{endpoint}")
            response.raise_for_status()
            return response.json()
        # FIXED: Catch specific exceptions
        except RequestException as e:
            print(f"Network error occurred: {e}")
            return {}
        except ValueError as e:
            print(f"JSON parsing error: {e}")
            return {}

    def send_data(self, endpoint: str, data: dict) -> bool:
        try:
            response = requests.post(f"{self.base_url}/{endpoint}", json=data)
            return response.status_code == 200
        except RequestException as e:
            print(f"Error: {e}")
            return False

    # Dummy method
    def parse_headers(self, headers):
        return dict(headers)
