package main

import (
	"reflect"
	"testing"
)

func TestConnectCommand(t *testing.T) {
	got := connectCommand("127.0.0.1", 22222, "root", "/tmp/id_ed25519")
	want := []string{"-p", "22222", "-i", "/tmp/id_ed25519", "root@127.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("connectCommand = %#v, want %#v", got, want)
	}
	if got := shellQuote("simple-name"); got != "simple-name" {
		t.Fatalf("shellQuote simple = %q", got)
	}
	if got := shellQuote("a b"); got != "'a b'" {
		t.Fatalf("shellQuote spaced = %q", got)
	}
}
