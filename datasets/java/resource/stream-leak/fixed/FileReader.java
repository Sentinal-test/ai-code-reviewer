package com.example.utils;

import java.io.FileInputStream;
import java.io.IOException;

public class FileReader {
    public byte[] readFile(String path) throws IOException {
        // FIXED: Use try-with-resources for automatic closing
        try (FileInputStream fis = new FileInputStream(path)) {
            byte[] content = new byte[fis.available()];
            fis.read(content);
            return content;
        }
    }

    public void logError(String msg) {
        System.err.println(msg);
    }
}
