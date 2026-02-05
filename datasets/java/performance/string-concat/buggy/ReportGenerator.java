package com.example.utils;

import java.util.List;

public class ReportGenerator {
    public String generateCsv(List<String[]> data) {
        // BUG: Inefficient String Concatenation
        // Using += in a loop creates a new String object every iteration.
        // O(N^2) performance when N is large.
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
