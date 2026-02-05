from typing import List, Dict

class DataFilter:
    def __init__(self, blocked_ids: List[int]):
        self.blocked_ids = blocked_ids

    def process_items(self, items: List[Dict]) -> List[Dict]:
        """
        Filters out items that have an ID in the blocked_ids list.
        """
        valid_items = []
        
        for item in items:
            item_id = item.get("id")
            # BUG: Performance O(N*M)
            # Checking membership in a list is O(N). Inside a loop, this becomes quadratic.
            # Should use a set for O(1) lookups.
            if item_id not in self.blocked_ids:
                valid_items.append(item)
                
        return valid_items

    def add_blocked_id(self, new_id: int):
        self.blocked_ids.append(new_id)

    def clear_blocked_ids(self):
        self.blocked_ids = []
