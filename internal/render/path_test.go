package render

import "testing"

func TestResolveWithinRootRejectsEscape(t *testing.T) {
	_, err := ResolveWithinRoot("/tmp/root", "../escape")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveWithinRootAcceptsChild(t *testing.T) {
	got, err := ResolveWithinRoot("/tmp/root", "apps/payments")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/tmp/root/apps/payments" {
		t.Fatalf("unexpected path: %s", got)
	}
}
