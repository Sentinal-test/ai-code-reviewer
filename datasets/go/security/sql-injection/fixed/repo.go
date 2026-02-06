package db

import (
	"database/sql"
)

type UserRepo struct {
	db *sql.DB
}

func (r *UserRepo) FindByEmail(email string) (*User, error) {
	query := "SELECT id, name, email FROM users WHERE email = ?"
	row := r.db.QueryRow(query, email)

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
