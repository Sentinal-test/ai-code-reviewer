package com.example.utils;

import java.util.List;

public class ReportGenerator {
    public String generateCsv(List<String[]> data) {
        // Using += in a loop creates a new String object every iteration.
        String csv = "";
        
        for (String[] row : data) {
            String line = String.join(",", row);
            csv += line + "\n";
        }
        
        return csv;
    }

    public String formatHeader(String[] headers) {
        return String.join(",", headers) + "\n";
    }
}
