from typing import List, Dict, Any
import time

class CacheManager:
    def __init__(self):
        self.hits = 0

    # The 'cache' dict is created once at definition time, not every call.
    def get_data(self, key: str, cache: Dict[str, Any] = {}) -> Any:
        if key in cache:
            self.hits += 1
            return cache[key]
        
        # Simulate expensive fetch
        data = self._fetch_from_db(key)
        cache[key] = data
        return data

    def _fetch_from_db(self, key: str) -> str:
        time.sleep(0.1)
        return f"value_{key}"

    def reset_stats(self):
        self.hits = 0

    def stats(self) -> str:
        return f"Hits: {self.hits}"
