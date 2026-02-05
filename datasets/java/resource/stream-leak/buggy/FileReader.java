package com.example.utils;

import java.io.FileInputStream;
import java.io.IOException;

public class FileReader {
    public byte[] readFile(String path) throws IOException {
        // BUG: Resource Leak
        // FileInputStream is opened but never closed.
        // Even if exception occurs during read, stream remains open.
        FileInputStream fis = new FileInputStream(path);
        byte[] content = new byte[fis.available()];
        fis.read(content);
        
        // Missing fis.close() or try-with-resources
        return content;
    }

    public void logError(String msg) {
        System.err.println(msg);
    }
}
