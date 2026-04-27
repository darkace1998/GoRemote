// Package plugin defines the cross-cutting plugin SDK: manifests, capabilities,
// trust levels, and lifecycle metadata shared by protocol and credential plugins.
//
// The plugin SDK is intentionally transport-agnostic. Built-in plugins satisfy
// these interfaces directly; out-of-process plugins satisfy them through an IPC
// shim (planned: gRPC/Connect over named pipes / Unix sockets).
package plugin

import (
	"context"
	"errors"
	"fmt"
)

// Kind identifies the broad category of a plugin.
type Kind string

const (
	KindProtocol   Kind = "protocol"
	KindCredential Kind = "credential"
)

// Trust describes how trusted a plugin is. Hosts use this to gate dangerous
// capabilities and decide whether to require explicit user confirmation.
type Trust string

const (
	TrustCore      Trust = "core"      // shipped in this binary
	TrustVerified  Trust = "verified"  // signed by a trusted publisher
	TrustCommunity Trust = "community" // unsigned, third-party
	TrustUntrusted Trust = "untrusted" // user-imported, default-deny
)

// Capability is a coarse permission a plugin may request. Hosts use this to
// implement capability-based security: only declared capabilities are granted.
type Capability string

const (
	CapNetworkOutbound  Capability = "network.outbound"
	CapClipboardRead    Capability = "clipboard.read"
	CapClipboardWrite   Capability = "clipboard.write"
	CapFilesystemRead   Capability = "fs.read"
	CapFilesystemWrite  Capability = "fs.write"
	CapKeychainRead     Capability = "keychain.read"
	CapKeychainWrite    Capability = "keychain.write"
	CapProcessSpawn     Capability = "process.spawn"
	CapOSExec           Capability = "os.exec"
	CapTerminal         Capability = "ui.terminal"
	CapGraphical        Capability = "ui.graphical"
	CapExternalLauncher Capability = "ui.external_launcher"
)

// Status reflects implementation maturity exposed by a plugin manifest.
type Status string

const (
	StatusReady        Status = "ready"
	StatusBeta         Status = "beta"
	StatusExperimental Status = "experimental"
	StatusPlanned      Status = "planned" // declared, not yet implemented
)

// Manifest is the static, declarative description of a plugin. Every plugin
// (built-in or external) must publish a manifest. Hosts validate manifests
// before activating the plugin.
type Manifest struct {
	// ID is a globally unique reverse-DNS-style identifier.
	// Example: "io.goremote.protocol.ssh".
	ID string `json:"id"`

	// Name is a short human-readable name shown in plugin lists.
	Name string `json:"name"`

	// Description is one or two sentences explaining what the plugin does.
	Description string `json:"description"`

	// Kind identifies which SDK contract the plugin implements.
	Kind Kind `json:"kind"`

	// Version follows semver; hosts use this for version negotiation.
	Version string `json:"version"`

	// APIVersion is the SDK contract version the plugin was built against.
	// Hosts refuse to load plugins built against incompatible API majors.
	APIVersion string `json:"api_version"`

	// Capabilities lists the permissions the plugin requests.
	Capabilities []Capability `json:"capabilities,omitempty"`

	// Platforms is a list of supported GOOS values; empty means "all".
	Platforms []string `json:"platforms,omitempty"`

	// Trust is set by the host loader, never the plugin itself.
	Trust Trust `json:"trust,omitempty"`

	// SignatureB64 is an optional Ed25519 signature over the canonical
	// JSON-serialised manifest body (all fields except SignatureB64 itself),
	// encoded as standard base64. When present it is verified against the
	// active trust policy before the plugin is activated.
	SignatureB64 string `json:"signature,omitempty"`

	// Status is set by the plugin author.
	Status Status `json:"status,omitempty"`

	// Publisher is informational metadata.
	Publisher string `json:"publisher,omitempty"`
	Homepage  string `json:"homepage,omitempty"`
	License   string `json:"license,omitempty"`
}

// Validate returns an error if the manifest is missing required fields or has
// invalid values. Hosts must call Validate before registering a plugin.
func (m *Manifest) Validate() error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if m.ID == "" {
		return errors.New("manifest.id is required")
	}
	if m.Name == "" {
		return errors.New("manifest.name is required")
	}
	if m.Kind != KindProtocol && m.Kind != KindCredential {
		return fmt.Errorf("manifest.kind must be %q or %q, got %q", KindProtocol, KindCredential, m.Kind)
	}
	if m.Version == "" {
		return errors.New("manifest.version is required")
	}
	if m.APIVersion == "" {
		return errors.New("manifest.api_version is required")
	}
	for _, cap := range m.Capabilities {
		if cap == "" {
			return errors.New("manifest.capabilities contains empty entry")
		}
	}
	return nil
}

// HasCapability reports whether the manifest declares the given capability.
func (m *Manifest) HasCapability(c Capability) bool {
	for _, x := range m.Capabilities {
		if x == c {
			return true
		}
	}
	return false
}

// Lifecycle is implemented by plugins that need explicit init/shutdown.
// Both Init and Shutdown must be safe to call once each. Plugins should
// honor ctx cancellation.
type Lifecycle interface {
	Init(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// Health is implemented by plugins reporting their runtime status.
type Health interface {
	Health(ctx context.Context) Status
}

// CurrentAPIVersion is the SDK API version this binary publishes.
// Bump the major when introducing breaking interface changes.
const CurrentAPIVersion = "1.0.0"
