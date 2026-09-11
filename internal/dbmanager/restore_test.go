package dbmanager

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestIsGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("dump")); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	if !isGzip(buf.Bytes()) {
		t.Error("gzipped data must be detected as gzip")
	}
	if isGzip([]byte("-- SQL comment\nCREATE TABLE t;")) {
		t.Error("plain sql must not be detected as gzip")
	}
	if isGzip(nil) {
		t.Error("empty data must not be detected as gzip")
	}
}

func TestGunzipIfNeeded(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte("CREATE TABLE items;")); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	sql, err := gunzipIfNeeded(buf.Bytes())
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(sql) != "CREATE TABLE items;" {
		t.Fatalf("decompressed = %q", string(sql))
	}

	// Plain SQL passes through untouched.
	sql, err = gunzipIfNeeded([]byte("SELECT 1;"))
	if err != nil {
		t.Fatalf("plain passthrough: %v", err)
	}
	if string(sql) != "SELECT 1;" {
		t.Fatalf("plain = %q", string(sql))
	}

	// Data that claims to be gzip but is corrupt must be rejected.
	if _, err := gunzipIfNeeded([]byte{0x1f, 0x8b, 0x00, 0x01}); err == nil {
		t.Error("corrupt gzip must return an error")
	}
}

func TestRestoreCommandPerEngine(t *testing.T) {
	bin, args, err := restoreCommand("mysql", "app_db")
	if err != nil || bin != "mysql" {
		t.Fatalf("mysql restore = %s %v, err %v", bin, args, err)
	}
	if strings.Join(args, " ") != "--database app_db" {
		t.Errorf("mysql args wrong: %v", args)
	}

	bin, args, err = restoreCommand("postgresql", "app_db")
	if err != nil || bin != "sudo" {
		t.Fatalf("postgresql restore = %s %v, err %v", bin, args, err)
	}
	if !containsArg(args, "ON_ERROR_STOP=on") {
		t.Error("psql must stop on the first error so failed restores are visible")
	}
	if !containsArg(args, "app_db") {
		t.Errorf("psql args missing database: %v", args)
	}

	if _, _, err := restoreCommand("redis", "cache"); err == nil {
		t.Error("redis restore must be rejected")
	}
}

func containsArg(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
