package com.example.api;

import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URL;

public class ApiClient {
    public void sendData(String endpoint, String data) {
        try {
            URL url = new URL(endpoint);
            HttpURLConnection conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod("POST");
            conn.setDoOutput(true);
            conn.getOutputStream().write(data.getBytes());
            conn.getInputStream().close();
        } catch (Exception e) {
            // BUG: Swallowed Exception
            // Exception is caught but nothing is done.
            // No logging, no rethrowing. Errors fail silently.
        }
    }

    public void validateUrl(String url) {
        // ...
    }
}
