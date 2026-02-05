package service

import "code-review/auth"

func ProcessRequest(token string) error {
	err := auth.Verify(token)
	if err != nil {
		// BUG: Doesn't handle ErrTokenExpired specifically,
		// just returns the raw error which might leak internals
		// or not allow the caller to retry.
		return err
	}
	return nil
}
