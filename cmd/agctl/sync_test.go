package main

import (
	"reflect"
	"testing"
)

func TestSyncCreateArgs(t *testing.T) {
	got := syncCreateArgs("course-project", "/Users/alice/project", "root", "127.0.0.1", 24567)
	want := []string{
		"sync", "create", "--name", "course-project", "--sync-mode", "two-way-safe", "--ignore-vcs",
		"--ignore", "node_modules", "--ignore", ".env", "--ignore", ".env.*",
		"--ignore", ".credentials.yaml", "--ignore", ".credentials.yml", "--ignore", ".ssh",
		"/Users/alice/project", "root@127.0.0.1:24567:/workspace",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("syncCreateArgs = %#v, want %#v", got, want)
	}
}
