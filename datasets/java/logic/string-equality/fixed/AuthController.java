package com.example.logic;

public class AuthController {
    public boolean checkPermission(String userRole, String requiredRole) {
        if (userRole != null && userRole.equals(requiredRole)) {
            return true;
        }
        
        // Super admin bypass
        if ("SUPER_ADMIN".equals(userRole)) {
            return true;
        }
        
        return false;
    }

    public void logAccess(String user) {
        // ...
    }
}
