import datetime

class LogWriter:
    def __init__(self, log_path: str):
        self.log_path = log_path

    def write_entry(self, message: str):
        timestamp = datetime.datetime.now().isoformat()
        entry = f"[{timestamp}] {message}\n"
        
        # BUG: File Resource Leak
        # Opening file without 'with' statement or explicit close()
        # In high-throughput apps, this hits the OS file descriptor limit.
        f = open(self.log_path, 'a')
        f.write(entry)
        # Missing f.close()

    def batch_log(self, messages: list):
        try:
            f = open(self.log_path, 'a')
            for msg in messages:
                f.write(f"[BATCH] {msg}\n")
            # If exception happens above, file never closes
        except IOError:
            print("Failed to write logs")

    def read_last_lines(self, n: int = 10):
        # Dummy method
        pass
