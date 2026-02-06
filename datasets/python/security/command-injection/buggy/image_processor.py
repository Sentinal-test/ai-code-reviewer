import os
import subprocess
from typing import Optional

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

        # If filename contains shell metacharacters (e.g., "test.jpg; rm -rf /"),
        # this will execute arbitrary commands.
        cmd = f"convert {input_path} -resize {width}x{height} {output_path}"
        
        try:
            subprocess.call(cmd, shell=True)
            return True
        except subprocess.CalledProcessError:
            return False

    def get_image_info(self, filename: str) -> Optional[str]:
        # Another dummy method
        return "Dummy Info"
