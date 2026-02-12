package models

import "time"

type User struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	IsGhost      bool      `json:"is_ghost"`
	CreatedAt    time.Time `json:"created_at"`
}
