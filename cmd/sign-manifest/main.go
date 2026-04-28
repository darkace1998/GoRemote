// sign-manifest signs an update manifest in-place using an Ed25519
// private key, producing the per-target Signature field consumed by
// app/update.Manifest.SelectTarget. Intended for use from the release
// workflow; not bundled into the desktop binary.
//
// Input manifest format (JSON):
//
//	{
//	  "version": "1.13.0",
//	  "notes":   "...",
//	  "targets": [
//	    {"os":"windows","arch":"amd64","url":"https://.../goremote-windows-amd64.exe","sha256":"deadbeef..."},
//	    ...
//	  ]
//	}
//
// The tool fills the "signature" field on each target by signing the
// canonical payload `version|os|arch|sha256|url` with the supplied
// Ed25519 private key (base64-encoded seed or full 64-byte key).
//
// Usage:
//
//	sign-manifest -in manifest.json -out manifest.signed.json -key $GOREMOTE_RELEASE_KEY
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
)

type manifestTarget struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature,omitempty"`
}

type manifest struct {
	Version string           `json:"version"`
	Notes   string           `json:"notes,omitempty"`
	Targets []manifestTarget `json:"targets"`
}

func main() {
	var (
		in     = flag.String("in", "", "input manifest JSON")
		out    = flag.String("out", "", "output manifest JSON (default: overwrite -in)")
		keyB64 = flag.String("key", os.Getenv("GOREMOTE_RELEASE_KEY"), "base64 Ed25519 private key (seed or full)")
	)
	flag.Parse()
	if *in == "" {
		fail("missing -in")
	}
	if *keyB64 == "" {
		fail("missing -key (or GOREMOTE_RELEASE_KEY env)")
	}
	if *out == "" {
		*out = *in
	}

	priv, err := decodeKey(*keyB64)
	if err != nil {
		fail("decode key: %v", err)
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		fail("read manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		fail("parse manifest: %v", err)
	}
	if m.Version == "" || len(m.Targets) == 0 {
		fail("manifest needs version and at least one target")
	}

	for i := range m.Targets {
		t := &m.Targets[i]
		if t.OS == "" || t.Arch == "" || t.URL == "" || t.SHA256 == "" {
			fail("target %d missing required field", i)
		}
		payload := fmt.Sprintf("%s|%s|%s|%s|%s", m.Version, t.OS, t.Arch, t.SHA256, t.URL)
		sig := ed25519.Sign(priv, []byte(payload))
		t.Signature = base64.StdEncoding.EncodeToString(sig)
	}

	enc, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fail("marshal: %v", err)
	}
	if err := os.WriteFile(*out, enc, 0o644); err != nil {
		fail("write: %v", err)
	}
	fmt.Fprintf(os.Stderr, "signed %d target(s) -> %s\n", len(m.Targets), *out)
}

func decodeKey(b64 string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, errors.New("key must be 32-byte seed or 64-byte private key")
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "sign-manifest: "+format+"\n", args...)
	os.Exit(1)
}
