package com.example.dao;

import java.sql.Connection;
import java.sql.ResultSet;
import java.sql.Statement;
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
        Connection conn = dataSource.getConnection();
        Statement stmt = conn.createStatement();
        
        // BUG: SQL Injection
        // User input 'query' is concatenated directly into the SQL string.
        String sql = "SELECT username FROM users WHERE username LIKE '" + query + "%'";
        
        ResultSet rs = stmt.executeQuery(sql);
        while (rs.next()) {
            results.add(rs.getString("username"));
        }
        
        rs.close();
        stmt.close();
        conn.close();
        
        return results;
    }

    // Dummy helper
    public void validateConnection() {
        // ... logic
    }
}
