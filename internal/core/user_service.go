package core

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
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

var validRoles = map[string]bool{
	"ACCOUNTANT":      true,
	"FINANCE_MANAGER": true,
	"ADMIN":           true,
}

func (s *userService) CreateUser(ctx context.Context, companyID int, params CreateUserParams) (*User, error) {
	if !validRoles[params.Role] {
		return nil, fmt.Errorf("invalid role %q: must be ACCOUNTANT, FINANCE_MANAGER, or ADMIN", params.Role)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(params.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	u := &User{}
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (company_id, username, email, password_hash, role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, company_id, username, email, password_hash, role, is_active, created_at`,
		companyID, params.Username, params.Email, string(hash), params.Role,
	).Scan(&u.ID, &u.CompanyID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user %q: %w", params.Username, err)
	}
	return u, nil
}

func (s *userService) UpdateUserRole(ctx context.Context, companyID, userID int, role string) error {
	if !validRoles[role] {
		return fmt.Errorf("invalid role %q: must be ACCOUNTANT, FINANCE_MANAGER, or ADMIN", role)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET role = $1
		WHERE id = $2 AND company_id = $3`,
		role, userID, companyID,
	)
	if err != nil {
		return fmt.Errorf("update role for user id=%d: %w", userID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user id=%d not found in company", userID)
	}
	return nil
}

func (s *userService) SetUserActive(ctx context.Context, companyID, userID int, active bool) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET is_active = $1
		WHERE id = $2 AND company_id = $3`,
		active, userID, companyID,
	)
	if err != nil {
		return fmt.Errorf("set active=%v for user id=%d: %w", active, userID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user id=%d not found in company", userID)
	}
	return nil
}

func (s *userService) ListUsers(ctx context.Context, companyID int) ([]*User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id, username, email, password_hash, role, is_active, created_at
		FROM users
		WHERE company_id = $1
		ORDER BY username`,
		companyID,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.CompanyID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
