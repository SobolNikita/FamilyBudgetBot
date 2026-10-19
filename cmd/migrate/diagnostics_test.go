package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
)

type testSQLState struct{}

func (testSQLState) Error() string    { return "secret-password" }
func (testSQLState) SQLState() string { return "28P01" }

func TestMigrationDiagnosticsIdentifyCausesWithoutSecrets(t *testing.T) {
	for _, sample := range []struct {
		err  error
		want string
	}{
		{&os.PathError{Op: "open", Path: "secret-password", Err: os.ErrNotExist}, "not found"},
		{&net.DNSError{Name: "secret-password", Err: "secret-password"}, "DNS lookup failed"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "connection refused"},
		{&net.DNSError{Name: "secret-password", IsTimeout: true}, "DNS lookup failed"},
		{&url.Error{Op: "parse", URL: "postgres://user:secret-password@db", Err: errors.New("secret-password")}, "invalid connection"},
		{testSQLState{}, "SQLSTATE 28P01"},
		{database.Error{Query: []byte("secret-password"), Err: "secret-password", OrigErr: testSQLState{}, Line: 12}, "line 12: PostgreSQL SQLSTATE 28P01"},
		{migrate.ErrDirty{Version: 7}, "dirty migration version 7"},
		{errors.New("secret-password"), "unclassified migration error"},
	} {
		got := migrationDiagnostic(fmt.Errorf("source contains secret-password: %w", sample.err))
		if !strings.Contains(got, sample.want) || strings.Contains(got, "secret-password") {
			t.Fatalf("unsafe or unhelpful diagnostic: %q, want %q", got, sample.want)
		}
	}
}
