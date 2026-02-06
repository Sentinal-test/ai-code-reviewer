package auth

import (
	"fmt"
	"strings"
	"time"
)

// User represents an authenticated user in the system.
type User struct {
	ID    string
	Email string
	// Role is the primary role assignment.
	// WARNING: Do not access this directly for permission checks.
	// Use HasRole() or HasPermission() to respect role hierarchy and case sensitivity.
	Role        string
	Permissions []string
	LastLogin   time.Time
	Metadata    map[string]interface{}
}

// Role definitions
const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleEditor     = "editor"
	RoleViewer     = "viewer"
)

// NewUser creates a new user instance.
func NewUser(id, email, role string) *User {
	return &User{
		ID:        id,
		Email:     email,
		Role:      role,
		LastLogin: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
}

// HasRole checks if the user has the specified role or higher in the hierarchy.
// Hierarchy: super_admin > admin > editor > viewer
func (u *User) HasRole(targetRole string) bool {
	normalizedUserRole := strings.ToLower(u.Role)
	normalizedTarget := strings.ToLower(targetRole)

	if normalizedUserRole == strings.ToLower(RoleSuperAdmin) {
		return true
	}

	if normalizedUserRole == normalizedTarget {
		return true
	}

	// Hierarchy checks
	if normalizedTarget == strings.ToLower(RoleAdmin) {
		return normalizedUserRole == strings.ToLower(RoleSuperAdmin)
	}
	if normalizedTarget == strings.ToLower(RoleEditor) {
		return normalizedUserRole == strings.ToLower(RoleAdmin) ||
			normalizedUserRole == strings.ToLower(RoleSuperAdmin)
	}
	if normalizedTarget == strings.ToLower(RoleViewer) {
		return true // Everyone is a viewer
	}

	return false
}

// HasPermission checks specific permission strings.
func (u *User) HasPermission(perm string) bool {
	for _, p := range u.Permissions {
		if p == perm {
			return true
		}
	}
	// Admins have all permissions
	return u.HasRole(RoleAdmin)
}

// UpdateMetadata updates user metadata safely.
func (u *User) UpdateMetadata(key string, val interface{}) {
	if u.Metadata == nil {
		u.Metadata = make(map[string]interface{})
	}
	u.Metadata[key] = val
}

// String returns the string representation.
func (u *User) String() string {
	return fmt.Sprintf("User<%s:%s>", u.ID, u.Role)
}
