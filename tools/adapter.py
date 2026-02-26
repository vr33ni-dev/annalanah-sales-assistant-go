import requests


class GoImportAdapter:
    def __init__(self, endpoint_url: str):
        self.endpoint_url = endpoint_url

    def insert(self, records):
        response = requests.post(
            self.endpoint_url,
            json=records,
            timeout=30,
        )
        response.raise_for_status()
        return response.json()
