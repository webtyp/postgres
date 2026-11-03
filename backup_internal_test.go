package postgres

import (

	"testing"
)

func TestNewerClient(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		server  int
		client  int
		newer   bool
		wantErr bool
	}{
		{"newer", "pg_restore (PostgreSQL) 17.11 (Debian...)", 14, 17, true, false},
		{"same", "pg_restore (PostgreSQL) 14.2", 14, 14, false, false},
		{"older", "pg_restore (PostgreSQL) 13.0", 14, 13, false, false},
		{"garbage", "garbage output", 14, 0, false, true},
		{"no major", "pg_restore (PostgreSQL) a.b.c", 14, 0, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, newer, err := newerClient(tt.output, tt.server)
			if (err != nil) != tt.wantErr {
				t.Errorf("newerClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if client != tt.client {
					t.Errorf("newerClient() client = %v, want %v", client, tt.client)
				}
				if newer != tt.newer {
					t.Errorf("newerClient() newer = %v, want %v", newer, tt.newer)
				}
			}
		})
	}
}

func TestOnlyTransactionTimeout(t *testing.T) {
	realOutput := `pg_restore: error: could not execute query: ERROR:  unrecognized configuration parameter "transaction_timeout"
pg_restore: warning: errors ignored on restore: 1`

	if !onlyTransactionTimeout(realOutput) {
		t.Errorf("onlyTransactionTimeout(realOutput) = false, want true")
	}

	badOutput := realOutput + `
pg_restore: error: could not execute query: ERROR: relation "x" does not exist`
	if onlyTransactionTimeout(badOutput) {
		t.Errorf("onlyTransactionTimeout(badOutput) = true, want false")
	}

	if onlyTransactionTimeout("") {
		t.Errorf("onlyTransactionTimeout(\"\") = true, want false")
	}
}
