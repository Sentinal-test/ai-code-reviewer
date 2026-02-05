package service

import "code-review/models"

func (s *UserService) NotifyUser(u *models.User) {
	println("Notifying: " + u.EmailAddr)
}
