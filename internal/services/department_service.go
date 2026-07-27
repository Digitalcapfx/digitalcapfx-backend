package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
)

// isUniqueViolation reports whether err is a Postgres unique-constraint (23505) error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

var (
	ErrDepartmentNotFound = errors.New("department not found")
	ErrDepartmentInUse    = errors.New("department has staff assigned — reassign them before deleting")
	ErrDepartmentExists   = errors.New("a department with that name already exists")
)

// DepartmentService is CRUD for admin team departments.
type DepartmentService struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func NewDepartmentService(pool *pgxpool.Pool, logger *zap.Logger) *DepartmentService {
	return &DepartmentService{pool: pool, logger: logger}
}

// Create adds a new department (name must be unique).
func (s *DepartmentService) Create(ctx context.Context, name, description string) (*db.AdminDepartment, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("department name is required")
	}
	q := db.New(s.pool)
	dep, err := q.CreateDepartment(ctx, db.CreateDepartmentParams{Name: name, Description: ptrOrNil(strings.TrimSpace(description))})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDepartmentExists
		}
		return nil, fmt.Errorf("create department: %w", err)
	}
	return &dep, nil
}

// List returns all departments with their staff counts.
func (s *DepartmentService) List(ctx context.Context) ([]db.ListDepartmentsRow, error) {
	q := db.New(s.pool)
	rows, err := q.ListDepartments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list departments: %w", err)
	}
	return rows, nil
}

// Get returns a single department.
func (s *DepartmentService) Get(ctx context.Context, id uuid.UUID) (*db.AdminDepartment, error) {
	q := db.New(s.pool)
	dep, err := q.GetDepartmentByID(ctx, id)
	if err != nil {
		return nil, ErrDepartmentNotFound
	}
	return &dep, nil
}

// Update changes a department's name and/or description.
func (s *DepartmentService) Update(ctx context.Context, id uuid.UUID, name, description *string) (*db.AdminDepartment, error) {
	q := db.New(s.pool)
	if _, err := q.GetDepartmentByID(ctx, id); err != nil {
		return nil, ErrDepartmentNotFound
	}
	dep, err := q.UpdateDepartment(ctx, db.UpdateDepartmentParams{ID: id, Name: name, Description: description})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDepartmentExists
		}
		return nil, fmt.Errorf("update department: %w", err)
	}
	return &dep, nil
}

// Delete removes a department. It refuses when staff are still assigned so the
// admin explicitly reassigns them first.
func (s *DepartmentService) Delete(ctx context.Context, id uuid.UUID) error {
	q := db.New(s.pool)
	count, err := q.CountStaffInDepartment(ctx, &id)
	if err != nil {
		return fmt.Errorf("count staff in department: %w", err)
	}
	if count > 0 {
		return ErrDepartmentInUse
	}
	rows, err := q.DeleteDepartment(ctx, id)
	if err != nil {
		return fmt.Errorf("delete department: %w", err)
	}
	if rows == 0 {
		return ErrDepartmentNotFound
	}
	return nil
}
