package service

import "code-review/models"

func (s *UserService) NotifyUser(u *models.User) {
	// BUG: u.Email no longer exists in models/user.go
	println("Notifying: " + u.Email)
}
