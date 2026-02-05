package com.example.dao;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.util.ArrayList;
import java.util.List;
import javax.sql.DataSource;

public class UserDao {
    private final DataSource dataSource;

    public UserDao(DataSource dataSource) {
        this.dataSource = dataSource;
    }

    public List<String> searchUsers(String query) throws Exception {
        List<String> results = new ArrayList<>();
        
        String sql = "SELECT username FROM users WHERE username LIKE ?";
        
        try (Connection conn = dataSource.getConnection();
             PreparedStatement pstmt = conn.prepareStatement(sql)) {
            
            pstmt.setString(1, query + "%");
            
            try (ResultSet rs = pstmt.executeQuery()) {
                while (rs.next()) {
                    results.add(rs.getString("username"));
                }
            }
        }
        
        return results;
    }

    // Dummy helper
    public void validateConnection() {
        // ... logic
    }
}
