package com.example.utils;

import java.io.FileInputStream;
import java.io.IOException;

public class FileReader {
    public byte[] readFile(String path) throws IOException {
        // Even if exception occurs during read, stream remains open.
        FileInputStream fis = new FileInputStream(path);
        byte[] content = new byte[fis.available()];
        fis.read(content);
        
        return content;
    }

    public void logError(String msg) {
        System.err.println(msg);
    }
}
