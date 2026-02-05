import threading
import time

class RequestCounter:
    def __init__(self):
        self.count = 0

    def process_request(self):
        # Simulate processing time
        time.sleep(0.001)
        
        # BUG: Race Condition
        # += is not atomic in Python (it's read-modify-write).
        # Multiple threads hitting this causes lost updates.
        self.count += 1
        
        return self.count

    def run_concurrently(self, n_threads: int):
        threads = []
        for _ in range(n_threads):
            t = threading.Thread(target=self.process_request)
            threads.append(t)
            t.start()
        
        for t in threads:
            t.join()
            
        print(f"Final count: {self.count}")
