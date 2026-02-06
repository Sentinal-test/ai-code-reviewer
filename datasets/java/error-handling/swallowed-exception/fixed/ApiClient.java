package com.example.api;

import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.logging.Level;
import java.util.logging.Logger;

public class ApiClient {
    private static final Logger LOGGER = Logger.getLogger(ApiClient.class.getName());

    public void sendData(String endpoint, String data) {
        try {
            URL url = new URL(endpoint);
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.getOutputStream().write(data.getBytes());
            conn.getInputStream().close();
        } catch (IOException e) {
            LOGGER.log(Level.SEVERE, "Failed to send data to API", e);
            // Optionally rethrow if caller needs to know
            // throw new RuntimeException(e);
        }
    }

    public void validateUrl(String url) {
        // ...
    }
}
