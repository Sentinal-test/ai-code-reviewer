package db

import (
	"database/sql"
	"fmt"
)

type UserRepo struct {
	db *sql.DB
}

func (r *UserRepo) FindByEmail(email string) (*User, error) {
	query := fmt.Sprintf("SELECT id, name, email FROM users WHERE email = '%s'", email)
	row := r.db.QueryRow(query)

	var user User
	err := row.Scan(&user.ID, &user.Name, &user.Email)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

type User struct {
	ID    int
	Name  string
	Email string
}
