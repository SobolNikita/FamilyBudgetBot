package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"syscall"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
)

// Driver errors can contain connection URLs, passwords or SQL values. Report
// typed causes rather than interpolating arbitrary error text into logs.
func migrationDiagnostic(err error) string {
	var queryError database.Error
	if errors.As(err, &queryError) {
		return fmt.Sprintf("SQL migration line %d: %s", queryError.Line, migrationDiagnostic(queryError.OrigErr))
	}
	var dirty migrate.ErrDirty
	if errors.As(err, &dirty) {
		return fmt.Sprintf("dirty migration version %d; inspect the failed migration before repairing its version", dirty.Version)
	}
	var sqlState interface{ SQLState() string }
	if errors.As(err, &sqlState) {
		code := sqlState.SQLState()
		if len(code) == 5 {
			valid := true
			for _, c := range code {
				valid = valid && (c >= '0' && c <= '9' || c >= 'A' && c <= 'Z')
			}
			if valid {
				return "PostgreSQL SQLSTATE " + code
			}
		}
		return "PostgreSQL error"
	}
	var dns *net.DNSError
	var network net.Error
	var parse *url.Error
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "migration file or directory not found; check MIGRATIONS_URL"
	case errors.Is(err, os.ErrPermission):
		return "permission denied; check migration file access"
	case errors.As(err, &dns):
		return "DNS lookup failed; check the database hostname and DNS"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "database connection refused; check host, port and database availability"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout():
		return "database connection timed out; check connectivity"
	case errors.As(err, &parse):
		return "invalid connection or migration URL; check DATABASE_URL and MIGRATIONS_URL"
	default:
		return fmt.Sprintf("unclassified migration error (%T); check database configuration and migration files", err)
	}
}
