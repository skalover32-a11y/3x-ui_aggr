package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSQLiteFilesFiltersRoots(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "data")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	db1 := filepath.Join(root, "main.db")
	db2 := filepath.Join(sub, "secondary.db")
	txt := filepath.Join(root, "note.txt")
	if err := os.WriteFile(db1, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db2, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(txt, []byte("txt"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := listSQLiteFiles([]string{root}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sqlite files, got %d", len(list))
	}
}

func TestResolveSQLiteTargetRejectsUnknown(t *testing.T) {
	root := t.TempDir()
	db1 := filepath.Join(root, "main.db")
	if err := os.WriteFile(db1, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveSQLiteTarget(sqliteStartRequest{Name: "missing.db"}, []string{root})
	if err == nil {
		t.Fatal("expected error for missing db name")
	}
	_, err = resolveSQLiteTarget(sqliteStartRequest{Path: "/etc/passwd"}, []string{root})
	if err == nil {
		t.Fatal("expected error for disallowed path")
	}
}

func TestResolveSQLiteTargetDuplicateName(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "one")
	sub2 := filepath.Join(root, "two")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "dup.db"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub2, "dup.db"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveSQLiteTarget(sqliteStartRequest{Name: "dup.db"}, []string{root})
	if err == nil {
		t.Fatal("expected error for duplicate db name")
	}
}
