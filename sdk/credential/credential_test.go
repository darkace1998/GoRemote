package credential

import "testing"

func TestZeroize(t *testing.T) {
	m := &Material{
		Username:   "u",
		Password:   "p",
		PrivateKey: []byte("secretkey"),
		Extra:      map[string]string{"otp": "123"},
	}
	m.Zeroize()
	if m.Username != "" || m.Password != "" {
		t.Fatal("strings not cleared")
	}
	if m.PrivateKey != nil {
		t.Fatal("private key slice not nil")
	}
	if m.Extra != nil {
		t.Fatal("extra not cleared")
	}
}

func TestZeroizeNil(t *testing.T) {
	var m *Material
	m.Zeroize() // must not panic
}
