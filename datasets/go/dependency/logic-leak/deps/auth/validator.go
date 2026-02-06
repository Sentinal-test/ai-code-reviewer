package auth

import "errors"

// ErrTokenExpired is thrown when a token is old
var ErrTokenExpired = errors.New("token expired")

func Verify(token string) error {
	if token == "expired" {
		return ErrTokenExpired
	}
	return nil
}
