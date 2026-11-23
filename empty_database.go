package postgres

import (
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"time"
)

// NewEmptyDatabase creates an empty database on the server dsn points to, named namePrefix plus a unique number, and
// returns its DSN and drop, which deletes it (connections included) and closes the admin connection. It is what a
// test run uses to start from a clean database of the real engine and leave nothing behind.
//
//	testDSN, drop, err := postgres.NewEmptyDatabase(dsn, "apptest_")
//	defer drop()
func NewEmptyDatabase(dsn, namePrefix string) (string, func() error, error) {
	u, err := parseURLDSN(dsn)
	if err != nil {
		return "", nil, err
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		return "", nil, err
	}
	target, drop, err := createEmptyDatabase(admin, u, namePrefix)
	if err != nil {
		return "", nil, errors.Join(err, admin.Close())
	}
	return target.String(), drop, nil
}

// parseURLDSN accepts only a postgres:// or postgresql:// URL: a key=value DSN has no path to swap the database in.
func parseURLDSN(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != urlSchemePG && u.Scheme != urlSchemePGSQL) {
		return nil, ErrDSNNotURL
	}
	return u, nil
}

// createEmptyDatabase creates namePrefix+<nanoseconds> through admin and returns the URL that points to it. drop
// deletes it and closes admin, so the caller hands admin over and never closes it itself.
func createEmptyDatabase(admin *sql.DB, u *url.URL, namePrefix string) (url.URL, func() error, error) {
	name := namePrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := admin.Exec("CREATE DATABASE " + quoteIdent(name)); err != nil {
		return url.URL{}, nil, err
	}
	target := *u
	target.Path = "/" + name
	drop := func() error {
		_, err := admin.Exec("DROP DATABASE IF EXISTS " + quoteIdent(name) + " WITH (FORCE)")
		return errors.Join(err, admin.Close())
	}
	return target, drop, nil
}
