package service

type UserService struct {
	cache map[string]bool
}

func (s *UserService) RegisterUser(email string) bool {
	if s.cache[email] {
		return false
	}
	s.cache[email] = true
	return true
}
