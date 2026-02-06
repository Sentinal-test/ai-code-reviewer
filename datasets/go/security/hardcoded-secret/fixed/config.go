package config

import "os"

func GetDatabaseURL() string {
	return os.Getenv("DATABASE_URL")
}

func GetAPIKey() string {
	return os.Getenv("API_KEY")
}
