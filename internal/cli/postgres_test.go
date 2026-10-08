package cli

import "testing"

func TestValidateLocalPostgresDSN(t *testing.T) {
	tests := map[string]bool{
		"postgres://u:p@127.0.0.1:5432/db":                                    true,
		"postgres://u:p@localhost:5432/db":                                    true,
		"postgres://u:p@[::1]:5432/db":                                        true,
		"postgres://u:p@db.example.com:5432/db":                               false,
		"host=127.0.0.1,db.example.com port=5432 user=u password=p dbname=db": false,
		"host=127.0.0.1,::1 port=5432 user=u password=p dbname=db":            true,
	}
	for dsn, want := range tests {
		if got := validateLocalPostgresDSN(dsn) == nil; got != want {
			t.Errorf("validateLocalPostgresDSN(%q) = %t, want %t", dsn, got, want)
		}
	}
}

func TestValidateLocalTemporalAddress(t *testing.T) {
	for address, want := range map[string]bool{
		"127.0.0.1:7233":        true,
		"localhost:7233":        true,
		"[::1]:7233":            true,
		"temporal.example:7233": false,
	} {
		if got := validateLocalTemporalAddress(address) == nil; got != want {
			t.Errorf("validateLocalTemporalAddress(%q) = %t, want %t", address, got, want)
		}
	}
}
