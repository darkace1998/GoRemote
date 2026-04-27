package platform

import "testing"

func TestNewProvider(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New returned nil")
	}
	if _, err := p.ConfigDir(); err != nil {
		t.Errorf("ConfigDir: %v", err)
	}
}
