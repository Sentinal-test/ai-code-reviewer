import datetime

class LogWriter:
    def __init__(self, log_path: str):
        self.log_path = log_path

    def write_entry(self, message: str):
        timestamp = datetime.datetime.now().isoformat()
        entry = f"[{timestamp}] {message}\n"
        
        with open(self.log_path, 'a') as f:
            f.write(entry)

    def batch_log(self, messages: list):
        try:
            with open(self.log_path, 'a') as f:
                for msg in messages:
                    f.write(f"[BATCH] {msg}\n")
        except IOError:
            print("Failed to write logs")

    def read_last_lines(self, n: int = 10):
        # Dummy method
        pass
