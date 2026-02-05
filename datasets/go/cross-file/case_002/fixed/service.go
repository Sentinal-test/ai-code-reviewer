package service

import "code-review/internal/db"

type UserService struct {
	db *db.Client
}

func (s *UserService) RegisterUser(email string) bool {
	// FIXED: Checks DB for global uniqueness
	exists, _ := s.db.CheckEmailExists(email)
	if exists {
		return false
	}
	s.db.SaveUser(email)
	return true
}
