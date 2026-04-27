package mosh

import (
	"context"
	"reflect"
	"testing"

	"github.com/goremote/goremote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestManifestValid(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
}

func TestBuildArgv_BasicHost(t *testing.T) {
	cfg := &config{Host: "host.example.com", Port: 22}
	got, err := buildArgv(cfg)
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	want := []string{"host.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v", got, want)
	}
}

func TestBuildArgv_PortAndUser(t *testing.T) {
	cfg := &config{Host: "host.example.com", Port: 2222, Username: "alice"}
	got, err := buildArgv(cfg)
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	want := []string{"--ssh=ssh -p 2222", "alice@host.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v", got, want)
	}
}

func TestBuildArgv_MoshPort(t *testing.T) {
	cfg := &config{Host: "h", Port: 22, MoshPort: 60001}
	got, err := buildArgv(cfg)
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	want := []string{"--port=60001", "h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v", got, want)
	}
}

func TestBuildArgv_SSHArgs(t *testing.T) {
	cfg := &config{Host: "h", Port: 22, SSHArgs: "-i ~/.ssh/id_rsa"}
	got, err := buildArgv(cfg)
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	want := []string{"--ssh=ssh -i ~/.ssh/id_rsa", "h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v", got, want)
	}
}

func TestBuildArgv_ExtraArgs(t *testing.T) {
	cfg := &config{
		Host:      "h",
		Port:      22,
		ExtraArgs: []string{"--no-init", "--predict=adaptive"},
	}
	got, err := buildArgv(cfg)
	if err != nil {
		t.Fatalf("buildArgv: %v", err)
	}
	if len(got) < 3 {
		t.Fatalf("expected at least 3 args, got %#v", got)
	}
	if got[len(got)-2] != "--no-init" || got[len(got)-1] != "--predict=adaptive" {
		t.Fatalf("extra_args not appended verbatim at end: %#v", got)
	}
}

func TestConfigFromRequest_MissingHost(t *testing.T) {
	_, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{},
	})
	if err == nil {
		t.Fatalf("expected error when host is missing")
	}
}

func TestConfigFromRequest_BadPort(t *testing.T) {
	_, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{
			SettingHost: "h",
			SettingPort: 99999,
		},
	})
	if err == nil {
		t.Fatalf("expected error for out-of-range port 99999")
	}
}

func TestOpen_BinaryNotFound(t *testing.T) {
	mod := &Module{
		discover: func(override string, candidates []string) (string, error) {
			return "", errInjected
		},
	}
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		Host:     "h",
		Settings: map[string]any{SettingHost: "h"},
	})
	if err == nil {
		t.Fatalf("expected discover error")
	}
}

var errInjected = injectedErr("injected")

type injectedErr string

func (e injectedErr) Error() string { return string(e) }
