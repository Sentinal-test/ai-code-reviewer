package com.example.logic;

public class AuthController {
    public boolean checkPermission(String userRole, String requiredRole) {
        // BUG: String Equality
        // Using == compares object references, not content.
        // Will fail if strings are created dynamically.
        if (userRole == requiredRole) {
            return true;
        }
        
        // Super admin bypass
        if (userRole == "SUPER_ADMIN") {
            return true;
        }
        
        return false;
    }

    public void logAccess(String user) {
        // ...
    }
}
