package app

import (
	"strings"
	"testing"
)

func TestUUIDAndActor(t *testing.T) {
	first, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(strings.Split(first, "-")) != 5 {
		t.Fatalf("invalid UUIDs: %q %q", first, second)
	}
	if defaultActor() == "" {
		t.Fatal("default actor is empty")
	}
}
