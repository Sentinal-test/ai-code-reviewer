from typing import List, Dict

class DataFilter:
    def __init__(self, blocked_ids: List[int]):
        self.blocked_ids = blocked_ids

    def process_items(self, items: List[Dict]) -> List[Dict]:
        """
        Filters out items that have an ID in the blocked_ids list.
        """
        valid_items = []
        
        # FIXED: Convert list to set for O(1) average time complexity lookups
        blocked_set = set(self.blocked_ids)
        
        for item in items:
            item_id = item.get("id")
            if item_id not in blocked_set:
                valid_items.append(item)
                
        return valid_items

    def add_blocked_id(self, new_id: int):
        self.blocked_ids.append(new_id)

    def clear_blocked_ids(self):
        self.blocked_ids = []
