package tests

import (
	"database/sql"
	"net/url"
	"testing"

	"webtyp.com/postgres"
)

// A test run starts from an empty database of the real engine and leaves nothing behind: NewEmptyDatabase gives a
// database with no tables, and drop deletes it.
func TestEmptyDatabase_CreatedEmptyAndDropped(t *testing.T) {
	dsn := backupDsnOrSkip(t)
	testDSN, drop, err := postgres.NewEmptyDatabase(dsn, "emptytest_")
	if err != nil {
		t.Fatalf("NewEmptyDatabase: %v", err)
	}
	db, err := sql.Open("postgres", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&tables); err != nil {
		t.Fatalf("new database not reachable: %v", err)
	}
	if tables != 0 {
		t.Errorf("new database has %d tables, want 0", tables)
	}
	db.Close()
	if err := drop(); err != nil {
		t.Fatalf("drop: %v", err)
	}
	u, _ := url.Parse(testDSN)
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var left int
	if err := admin.QueryRow("SELECT count(*) FROM pg_database WHERE datname = $1", u.Path[1:]).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("database %s still exists after drop", u.Path[1:])
	}
}
