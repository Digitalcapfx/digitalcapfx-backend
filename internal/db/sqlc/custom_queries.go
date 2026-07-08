package db

import (
	"errors"
)

var errNotImplemented = errors.New("sqlc: not generated — run make sqlc")
