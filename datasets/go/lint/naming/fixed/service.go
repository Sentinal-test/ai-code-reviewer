package service

import "database/sql"

type UserData struct {
	UserID    int
	UserName  string
	UserEmail string
}

func GetUserByID(db *sql.DB, id int) *UserData {
	var u UserData
	db.QueryRow("SELECT user_id, user_name, user_email FROM users WHERE user_id = ?", id).Scan(&u.UserID, &u.UserName, &u.UserEmail)
	return &u
}

func UpdateUser(db *sql.DB, data *UserData) error {
	_, err := db.Exec("UPDATE users SET user_name = ?, user_email = ? WHERE user_id = ?", data.UserName, data.UserEmail, data.UserID)
	return err
}
