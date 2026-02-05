import asyncio
import time
from fastapi import FastAPI

app = FastAPI()

class NotificationService:
    def __init__(self):
        self.queue = []

    async def send_notification(self, email: str, message: str):
        """
        Sends an email notification.
        """
        # BUG: Blocking code in async function
        # time.sleep blocks the entire event loop, freezing all other requests.
        # Should use asyncio.sleep() for non-blocking delay.
        time.sleep(5) 
        
        print(f"Sent email to {email}: {message}")
        return {"status": "sent"}

    async def batch_process(self, emails: list):
        for email in emails:
            await self.send_notification(email, "Update available")

# Routes
@app.post("/notify")
async def notify_user(email: str):
    service = NotificationService()
    return await service.send_notification(email, "Hello")

@app.get("/health")
async def health_check():
    return {"status": "ok"}
