package apperrors

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ClassValidation          = "validation"
	ClassNotFound            = "not_found"
	ClassDatabaseQuery       = "database_query"
	ClassDatabaseScan        = "database_scan"
	ClassDatabaseUnavailable = "database_unavailable"
	ClassMapping             = "mapping"
	ClassInvariantViolation  = "invariant_violation"
	ClassTimeout             = "timeout"
	ClassCancelled           = "cancelled"
	ClassUnexpected          = "unexpected"
)

type OpError struct {
	Operation string
	Class     string
	Err       error
}

func (e *OpError) Error() string { return e.Operation + ": " + e.Err.Error() }
func (e *OpError) Unwrap() error { return e.Err }

func Wrap(operation, class string, err error) error {
	if err == nil {
		return nil
	}
	return &OpError{Operation: operation, Class: class, Err: err}
}

func Classify(err error) (class, op string) {
	class = ClassUnexpected
	var oe *OpError
	if errors.As(err, &oe) {
		if oe.Class != "" {
			class = oe.Class
		}
		op = oe.Operation
	}
	if errors.Is(err, context.Canceled) {
		return ClassCancelled, op
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassTimeout, op
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassNotFound, op
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return ClassDatabaseQuery, op
	}
	if strings.Contains(err.Error(), "Scan error") || strings.Contains(err.Error(), "can't scan") || strings.Contains(err.Error(), "cannot scan") {
		return ClassDatabaseScan, op
	}
	return class, op
}
