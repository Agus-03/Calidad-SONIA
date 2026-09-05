package db

import (
	"testing"
	"testing/fstest"
)

// The numeric prefix is the order, and a file that is not .sql is documentation and not DDL.
func TestMigrationNamesAreSortedAndFiltered(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_operators.sql": {Data: []byte("")},
		"0001_accounts.sql":  {Data: []byte("")},
		"README.md":          {Data: []byte("")},
		"0010_labels.sql":    {Data: []byte("")},
	}

	got, err := migrationNames(fsys)
	if err != nil {
		t.Fatalf("migrationNames: got %v, want no error", err)
	}

	want := []string{"0001_accounts.sql", "0002_operators.sql", "0010_labels.sql"}
	if len(got) != len(want) {
		t.Fatalf("migrationNames: got %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("migrationNames[%d]: got %q, want %q", i, got[i], name)
		}
	}
}

// Git hands the same file as CRLF on Windows and as LF in the container, and a boot must not read that as an edit.
func TestChecksumIgnoresLineEndings(t *testing.T) {
	crlf := checksum([]byte("CREATE TABLE a ();\r\nCREATE TABLE b ();\r\n"))
	lf := checksum([]byte("CREATE TABLE a ();\nCREATE TABLE b ();\n"))

	if crlf != lf {
		t.Fatalf("checksum: got %s and %s, want the same value for both line endings", crlf, lf)
	}
}

// A real edit has to change the checksum, or the guard that compares it protects nothing.
func TestChecksumChangesWithTheContent(t *testing.T) {
	before := checksum([]byte("CREATE TABLE a ();\n"))
	after := checksum([]byte("CREATE TABLE a (id uuid);\n"))

	if before == after {
		t.Fatalf("checksum: got %s for two different files, want different values", before)
	}
}
