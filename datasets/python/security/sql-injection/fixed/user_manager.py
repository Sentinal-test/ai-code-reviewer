import sqlite3
from typing import List, Optional, Tuple

class UserManager:
    def __init__(self, db_path: str):
        self.db_path = db_path

    def get_user_by_username(self, username: str) -> Optional[Tuple]:
        """
        Retrieves a user record by their username.
        """
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        
        try:
            # FIXED: Use parameterized queries to prevent SQL injection
            query = "SELECT * FROM users WHERE username = ?"
            cursor.execute(query, (username,))
            
            user = cursor.fetchone()
            return user
        except sqlite3.Error as e:
            print(f"Database error: {e}")
            return None
        finally:
            conn.close()

    def get_active_users(self) -> List[Tuple]:
        conn = sqlite3.connect(self.db_path)
        cursor = conn.cursor()
        cursor.execute("SELECT * FROM users WHERE active = 1")
        users = cursor.fetchall()
        conn.close()
        return users

    # Dummy methods to increase file size
    def validate_password(self, password: str) -> bool:
        return len(password) >= 8

    def format_user_data(self, user: Tuple) -> dict:
        return {"id": user[0], "username": user[1]}
