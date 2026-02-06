package service

import "database/sql"

type user_data struct {
	user_id    int
	user_name  string
	user_email string
}

func get_user_by_id(DB *sql.DB, ID int) *user_data {
	var u user_data
	DB.QueryRow("SELECT user_id, user_name, user_email FROM users WHERE user_id = ?", ID).Scan(&u.user_id, &u.user_name, &u.user_email)
	return &u
}

func Update_User(DB *sql.DB, data *user_data) error {
	_, err := DB.Exec("UPDATE users SET user_name = ?, user_email = ? WHERE user_id = ?", data.user_name, data.user_email, data.user_id)
	return err
}

var GlobalDB *sql.DB
