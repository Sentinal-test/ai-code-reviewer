package service

import "errors"

type User struct {
	ID   int
	Name string
}

type Repo interface {
	Find(id int) (*User, error)
}

type UserService struct {
	repo Repo
}

func (s *UserService) CreateUser(id int) (string, error) {
	user, err := s.repo.Find(id)
	if err != nil {
		return "", err
	}

	if user == nil {
		return "", errors.New("user not found")
	}

	return user.Name, nil
}
