package cli

import "testing"

func TestValidateLocalPostgresDSN(t *testing.T) {
	tests := map[string]bool{
		"postgres://u:p@127.0.0.1:5432/db":      true,
		"postgres://u:p@localhost:5432/db":      true,
		"postgres://u:p@[::1]:5432/db":          true,
		"postgres://u:p@db.example.com:5432/db": false,
	}
	for dsn, want := range tests {
		if got := validateLocalPostgresDSN(dsn) == nil; got != want {
			t.Errorf("validateLocalPostgresDSN(%q) = %t, want %t", dsn, got, want)
		}
	}
}
