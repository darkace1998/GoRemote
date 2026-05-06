package main

import "testing"

func TestResolveKey_FlagTakesPrecedence(t *testing.T) {
	got := resolveKey("from-flag", "from-env")
	if got != "from-flag" {
		t.Errorf("want %q, got %q", "from-flag", got)
	}
}

func TestResolveKey_FallsBackToEnv(t *testing.T) {
	got := resolveKey("", "from-env")
	if got != "from-env" {
		t.Errorf("want %q, got %q", "from-env", got)
	}
}

func TestResolveKey_BothEmpty(t *testing.T) {
	got := resolveKey("", "")
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
