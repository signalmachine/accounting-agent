package core

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type userService struct {
	pool *pgxpool.Pool
}

// NewUserService constructs a UserService backed by PostgreSQL.
func NewUserService(pool *pgxpool.Pool) UserService {
	return &userService{pool: pool}
}

func (s *userService) GetByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, company_id, username, email, password_hash, role, is_active, created_at
		FROM users
		WHERE username = $1 AND is_active = true
		LIMIT 1`,
		username,
	).Scan(&u.ID, &u.CompanyID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("user %q not found: %w", username, err)
	}
	return u, nil
}

func (s *userService) GetByID(ctx context.Context, userID int) (*User, error) {
	u := &User{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, company_id, username, email, password_hash, role, is_active, created_at
		FROM users
		WHERE id = $1`,
		userID,
	).Scan(&u.ID, &u.CompanyID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("user id=%d not found: %w", userID, err)
	}
	return u, nil
}
