package service

import (
	"code-review/auth"
	"errors"
)

func ProcessRequest(token string) error {
	err := auth.Verify(token)
	if err != nil {
		if errors.Is(err, auth.ErrTokenExpired) {
			return errors.New("please login again")
		}
		return err
	}
	return nil
}
