package models

type User struct {
	ID        int
	EmailAddr string // Field renamed from Email to EmailAddr
	Name      string
}
