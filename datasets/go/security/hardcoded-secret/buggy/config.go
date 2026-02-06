package config

var DatabaseURL = "postgres://admin:supersecretpassword123@localhost:5432/app"
var APIKey = "sk-live-abc123xyz789"

func GetDatabaseURL() string {
	return DatabaseURL
}

func GetAPIKey() string {
	return APIKey
}
