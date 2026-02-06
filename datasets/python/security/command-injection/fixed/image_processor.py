import os
import subprocess
from typing import Optional
import shlex

class ImageProcessor:
    def __init__(self, upload_dir: str):
        self.upload_dir = upload_dir

    def resize_image(self, filename: str, width: int, height: int) -> bool:
        """
        Resizes an image using ImageMagick's convert command.
        """
        if not filename.endswith(('.jpg', '.png', '.jpeg')):
            return False

        input_path = os.path.join(self.upload_dir, filename)
        output_path = os.path.join(self.upload_dir, f"resized_{filename}")

        cmd = ["convert", input_path, "-resize", f"{width}x{height}", output_path]
        
        try:
            # shell=False is the default, ensuring arguments are passed safely
            subprocess.call(cmd, shell=False)
            return True
        except subprocess.CalledProcessError:
            return False

    def get_image_info(self, filename: str) -> Optional[str]:
        # Another dummy method
        return "Dummy Info"
