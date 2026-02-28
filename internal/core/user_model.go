package core

import (
	"context"
	"time"
)

// User represents an authenticated system user scoped to a company.
type User struct {
	ID           int
	CompanyID    int
	Username     string
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	CreatedAt    time.Time
}

// UserService provides user lookup operations.
type UserService interface {
	// GetByUsername finds an active user by username (global lookup — single-company MVP).
	GetByUsername(ctx context.Context, username string) (*User, error)

	// GetByID returns a user by primary key.
	GetByID(ctx context.Context, userID int) (*User, error)
}
