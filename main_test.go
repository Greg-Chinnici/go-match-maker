package main

import (
	"net/url"
	"testing"
)

// setDBEnv sets every variable postgresConnStr reads, so a local .env
// (which never overrides set variables) can't leak into the test.
func setDBEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range []string{"DATABASE_URL", "DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT", "DB_NAME", "DB_SSLMODE"} {
		t.Setenv(k, vars[k])
	}
}

func TestPostgresConnStr(t *testing.T) {
	t.Run("built from DB_ vars with escaping", func(t *testing.T) {
		setDBEnv(t, map[string]string{
			"DB_USER": "postgres", "DB_PASSWORD": "p@ss/w:rd", "DB_HOST": "localhost", "DB_PORT": "5432", "DB_NAME": "games",
		})
		got, err := postgresConnStr()
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("unparseable %q: %v", got, err)
		}
		if pass, _ := u.User.Password(); pass != "p@ss/w:rd" {
			t.Errorf("password round-tripped as %q", pass)
		}
		if u.Host != "localhost:5432" || u.Path != "/games" || u.Query().Get("sslmode") != "disable" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("DB_SSLMODE", func(t *testing.T) {
		setDBEnv(t, map[string]string{"DB_USER": "u", "DB_HOST": "db.example.com", "DB_PORT": "5432", "DB_SSLMODE": "require"})
		got, _ := postgresConnStr()
		if u, _ := url.Parse(got); u.Query().Get("sslmode") != "require" {
			t.Errorf("got %q, want sslmode=require", got)
		}
	})

	t.Run("DATABASE_URL wins", func(t *testing.T) {
		hosted := "postgres://app:secret@db.example.com:5432/prod?sslmode=require"
		setDBEnv(t, map[string]string{"DATABASE_URL": hosted, "DB_USER": "ignored", "DB_HOST": "localhost"})
		if got, _ := postgresConnStr(); got != hosted {
			t.Errorf("got %q, want %q", got, hosted)
		}
	})

	t.Run("nothing configured", func(t *testing.T) {
		setDBEnv(t, nil)
		if _, err := postgresConnStr(); err == nil {
			t.Error("expected an error")
		}
	})
}
