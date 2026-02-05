package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/repo/internal/auth"
)

type AdminHandler struct {
	auditLog []string
}

func NewAdminHandler() *AdminHandler {
	return &AdminHandler{
		auditLog: make([]string, 0),
	}
}

// HandleDeleteUser processes user deletion requests.
// Only admins should be able to delete users.
func (h *AdminHandler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	// Simulate extracting user from context (set by middleware)
	ctxUser, ok := r.Context().Value("user").(*auth.User)
	if !ok || ctxUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Fixed: Use HasRole() to respect hierarchy (SuperAdmin includes Admin) and case sensitivity.
	if !ctxUser.HasRole(auth.RoleAdmin) {
		http.Error(w, "Forbidden: Admins only", http.StatusForbidden)
		return
	}

	targetID := r.URL.Query().Get("id")
	if targetID == "" {
		http.Error(w, "Missing user ID", http.StatusBadRequest)
		return
	}

	// Perform deletion logic...
	h.logAction(fmt.Sprintf("User %s deleted user %s", ctxUser.Email, targetID))

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

// HandleSystemConfig updates system settings.
func (h *AdminHandler) HandleSystemConfig(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := r.Context().Value("user").(*auth.User)
	if !ok || ctxUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Fixed: Check for minimum required role (Editor) instead of blocking specific role
	if !ctxUser.HasRole(auth.RoleEditor) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Update config logic...
	h.logAction("Config updated")
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) logAction(action string) {
	entry := fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), action)
	h.auditLog = append(h.auditLog, entry)
	fmt.Println(entry)
}

// Dummy methods to increase file size...
func (h *AdminHandler) Helper1() { time.Sleep(time.Millisecond) }
func (h *AdminHandler) Helper2() { time.Sleep(time.Millisecond) }
func (h *AdminHandler) Helper3() { time.Sleep(time.Millisecond) }
func (h *AdminHandler) Helper4() { time.Sleep(time.Millisecond) }
func (h *AdminHandler) Helper5() { time.Sleep(time.Millisecond) }
