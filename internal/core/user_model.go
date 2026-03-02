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

// CreateUserParams contains the fields needed to create a new user.
type CreateUserParams struct {
	Username string
	Email    string
	Password string // plain-text; the service hashes it
	Role     string // ACCOUNTANT | FINANCE_MANAGER | ADMIN
}

// UserService provides user lookup and management operations.
type UserService interface {
	// GetByUsername finds an active user by username (global lookup — single-company MVP).
	GetByUsername(ctx context.Context, username string) (*User, error)

	// GetByID returns a user by primary key.
	GetByID(ctx context.Context, userID int) (*User, error)

	// CreateUser creates a new user for the given company, hashing the password.
	CreateUser(ctx context.Context, companyID int, params CreateUserParams) (*User, error)

	// ListUsers returns all users for the given company, ordered by username.
	ListUsers(ctx context.Context, companyID int) ([]*User, error)

	// UpdateUserRole changes the role of a user, scoped to a company.
	UpdateUserRole(ctx context.Context, companyID, userID int, role string) error

	// SetUserActive activates or deactivates a user, scoped to a company.
	SetUserActive(ctx context.Context, companyID, userID int, active bool) error
}
