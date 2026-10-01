package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// The filename is the version, and the version is what the server records and
// what orders the sequence. Getting the parsing or the ordering wrong applies
// migrations out of order, which is the one failure a migration tool must not
// have.

func TestLocalMigrationsAreOrderedByVersionNotByName(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Deliberately named so alphabetical order and chronological order disagree:
	// "add_..." sorts before "create_..." but happened after it.
	write(t, "20260101000000_create_users.sql", "create table users();")
	write(t, "20260201000000_add_email.sql", "alter table users add email text;")
	write(t, "20260301000000_add_index.sql", "create index on users(email);")

	got, err := localMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("found %d migrations, want 3", len(got))
	}
	want := []string{"20260101000000", "20260201000000", "20260301000000"}
	for i, v := range want {
		if got[i].Version != v {
			t.Errorf("position %d was %s, want %s — migrations would apply out of order", i, got[i].Version, v)
		}
	}
	if got[0].Name != "create_users" {
		t.Errorf("name parsed as %q, want create_users", got[0].Name)
	}
}

func TestLocalMigrationsIgnoresFilesThatAreNotMigrations(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	write(t, "20260101000000_real.sql", "select 1;")
	// A README, an editor backup, and a down file must not be picked up as
	// migrations. The down file especially: applying it would undo the
	// migration it belongs to.
	write(t, "README.md", "notes")
	write(t, "20260101000000_real.sql.bak", "select 1;")
	write(t, "20260101000000_real.down.sql", "drop table x;")

	got, err := localMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		names := []string{}
		for _, g := range got {
			names = append(names, filepath.Base(g.Path))
		}
		t.Fatalf("picked up %v, want only the one migration", names)
	}
}

func TestLocalMigrationsIsEmptyRatherThanFailingWithNoDirectory(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// A project that has never made a migration is not an error state — the
	// commands should say "none yet", not fail.
	got, err := localMigrations()
	if err != nil {
		t.Fatalf("a missing directory should not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d migrations from an empty directory", len(got))
	}
}

func TestSlugifyProducesAFilenameSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"Create Employees Table": "create_employees_table",
		"add-email--column":      "add_email_column",
		"  spaced  ":             "spaced",
		"weird!!chars??":         "weird_chars",
		"CamelCase":              "camelcase",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
	// A name of only punctuation has nothing left, and the command refuses
	// rather than writing a file called "20260101000000_.sql".
	if got := slugify("!!!"); got != "" {
		t.Errorf("slugify(%q) = %q, want empty so the command can refuse", "!!!", got)
	}
}

func write(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(migrationsDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
