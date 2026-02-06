package com.example.utils;

import java.util.List;

public class ReportGenerator {
    public String generateCsv(List<String[]> data) {
        StringBuilder sb = new StringBuilder();
        
        for (String[] row : data) {
            sb.append(String.join(",", row));
            sb.append("\n");
        }
        
        return sb.toString();
    }

    public String formatHeader(String[] headers) {
        return String.join(",", headers) + "\n";
    }
}
