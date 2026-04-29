package domain

import (
	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Field identifies a ConnectionNode field that participates in inheritance.
type Field string

// Inheritable fields.
const (
	FieldProtocolID Field = "protocol_id"
	FieldHost       Field = "host"
	FieldPort       Field = "port"
	FieldUsername   Field = "username"
	FieldAuthMethod Field = "auth_method"
	// #nosec G101 -- Field names describe schema keys; this is not embedded credential data.
	FieldCredentialRef Field = "credential_ref"
	FieldSettings      Field = "settings"
	FieldTags          Field = "tags"
	FieldColor         Field = "color"
	FieldIcon          Field = "icon"
	FieldEnvironment   Field = "environment"
	FieldDescription   Field = "description"
)

// AllInheritableFields lists every Field the resolver understands.
var AllInheritableFields = []Field{
	FieldProtocolID,
	FieldHost,
	FieldPort,
	FieldUsername,
	FieldAuthMethod,
	FieldCredentialRef,
	FieldSettings,
	FieldTags,
	FieldColor,
	FieldIcon,
	FieldEnvironment,
	FieldDescription,
}

// InheritanceProfile declares, per field, whether the ConnectionNode's own
// value should be used ("explicit") or whether the resolver should walk the
// ancestor folder chain looking for a value ("inherit").
//
// A field missing from both maps defaults to: use the node's own value if it
// is non-zero, otherwise walk the ancestor chain.
type InheritanceProfile struct {
	// Explicit[f] forces the node's own value to win, even if it is zero.
	Explicit map[Field]bool
	// Inherit[f] forces the resolver to ignore the node's value and walk
	// ancestors. If nothing is found, the field is left at the zero value.
	Inherit map[Field]bool
}

// SetExplicit marks a field as explicit on the node.
func (p *InheritanceProfile) SetExplicit(f Field) {
	if p.Explicit == nil {
		p.Explicit = make(map[Field]bool)
	}
	p.Explicit[f] = true
	delete(p.Inherit, f)
}

// SetInherit marks a field as inherited from ancestors.
func (p *InheritanceProfile) SetInherit(f Field) {
	if p.Inherit == nil {
		p.Inherit = make(map[Field]bool)
	}
	p.Inherit[f] = true
	delete(p.Explicit, f)
}

// ProvenanceSource categorises the origin of a resolved value.
type ProvenanceSource string

// Known provenance sources.
const (
	// ProvenanceNode means the value came from the ConnectionNode itself.
	ProvenanceNode ProvenanceSource = "node"
	// ProvenanceFolder means the value was inherited from an ancestor folder.
	ProvenanceFolder ProvenanceSource = "folder"
	// ProvenanceDefault means no explicit or inherited value was found.
	ProvenanceDefault ProvenanceSource = "default"
)

// Provenance identifies where a resolved field value came from.
type Provenance struct {
	// Field is the resolved field.
	Field Field
	// Source describes the origin category.
	Source ProvenanceSource
	// FolderID is set when Source == ProvenanceFolder.
	FolderID ID
}

// ResolvedConnection is the effective connection after inheritance has been
// applied. Trace reports, per field, where each effective value came from.
type ResolvedConnection struct {
	ConnectionNode
	// Trace maps each resolved Field to its Provenance entry.
	Trace map[Field]Provenance
}

// Resolve walks the ancestor chain and produces the effective ConnectionNode.
//
// The ancestors slice must be ordered root-first (top-down): ancestors[0] is
// the top-most ancestor, ancestors[len-1] is the node's immediate parent.
// Callers that have the list parent-first can use ReverseFolders.
//
// Rules per field:
//   - p.Explicit[f] true  -> the node's value wins.
//   - p.Inherit[f]  true  -> the node's value is ignored; the nearest
//     ancestor (scanning from the immediate parent upward) with a non-zero
//     value wins. If none, the field becomes zero.
//   - neither set         -> node's value wins if non-zero; otherwise nearest
//     ancestor with a non-zero value wins; otherwise zero.
func (p InheritanceProfile) Resolve(node *ConnectionNode, ancestors []*FolderNode) ResolvedConnection {
	res := ResolvedConnection{Trace: make(map[Field]Provenance, len(AllInheritableFields))}
	if node == nil {
		return res
	}
	res.ConnectionNode = *node

	for _, f := range AllInheritableFields {
		src, folderID := p.resolveInto(&res.ConnectionNode, f, node, ancestors)
		res.Trace[f] = Provenance{Field: f, Source: src, FolderID: folderID}
	}
	return res
}

// resolveInto computes the effective value for field f, writes it into out,
// and returns its provenance.
func (p InheritanceProfile) resolveInto(out *ConnectionNode, f Field, node *ConnectionNode, ancestors []*FolderNode) (ProvenanceSource, ID) {
	forceExplicit := p.Explicit[f]
	forceInherit := p.Inherit[f]

	if forceExplicit {
		copyNodeField(out, node, f)
		return ProvenanceNode, NilID
	}
	if !forceInherit && !isNodeFieldZero(node, f) {
		copyNodeField(out, node, f)
		return ProvenanceNode, NilID
	}

	// Walk ancestors nearest-first (immediate parent is last in the slice).
	for i := len(ancestors) - 1; i >= 0; i-- {
		a := ancestors[i]
		if a == nil {
			continue
		}
		if !isFolderDefaultZero(a, f) {
			copyFolderField(out, a, f)
			return ProvenanceFolder, a.ID
		}
	}

	// Nothing inherited; either force-inherit (-> zero) or keep node's zero.
	if forceInherit {
		zeroNodeField(out, f)
	} else {
		copyNodeField(out, node, f)
	}
	return ProvenanceDefault, NilID
}

// ReverseFolders returns a new slice containing the input in reverse order.
func ReverseFolders(in []*FolderNode) []*FolderNode {
	out := make([]*FolderNode, len(in))
	for i, f := range in {
		out[len(in)-1-i] = f
	}
	return out
}

func isNodeFieldZero(n *ConnectionNode, f Field) bool {
	switch f {
	case FieldProtocolID:
		return n.ProtocolID == ""
	case FieldHost:
		return n.Host == ""
	case FieldPort:
		return n.Port == 0
	case FieldUsername:
		return n.Username == ""
	case FieldAuthMethod:
		return n.AuthMethod == ""
	case FieldCredentialRef:
		return isRefZero(n.CredentialRef)
	case FieldSettings:
		return len(n.Settings) == 0
	case FieldTags:
		return len(n.Tags) == 0
	case FieldColor:
		return n.Color == ""
	case FieldIcon:
		return n.Icon == ""
	case FieldEnvironment:
		return n.Environment == ""
	case FieldDescription:
		return n.Description == ""
	}
	return true
}

func isFolderDefaultZero(f *FolderNode, fd Field) bool {
	d := f.Defaults
	switch fd {
	case FieldProtocolID:
		return d.ProtocolID == ""
	case FieldHost:
		return d.Host == ""
	case FieldPort:
		return d.Port == 0
	case FieldUsername:
		return d.Username == ""
	case FieldAuthMethod:
		return d.AuthMethod == ""
	case FieldCredentialRef:
		return isRefZero(d.CredentialRef)
	case FieldSettings:
		return len(d.Settings) == 0
	case FieldTags:
		return len(d.Tags) == 0
	case FieldColor:
		return d.Color == ""
	case FieldIcon:
		return d.Icon == ""
	case FieldEnvironment:
		return d.Environment == ""
	case FieldDescription:
		return d.Description == ""
	}
	return true
}

func copyNodeField(dst, src *ConnectionNode, f Field) {
	switch f {
	case FieldProtocolID:
		dst.ProtocolID = src.ProtocolID
	case FieldHost:
		dst.Host = src.Host
	case FieldPort:
		dst.Port = src.Port
	case FieldUsername:
		dst.Username = src.Username
	case FieldAuthMethod:
		dst.AuthMethod = src.AuthMethod
	case FieldCredentialRef:
		dst.CredentialRef = cloneRef(src.CredentialRef)
	case FieldSettings:
		dst.Settings = cloneSettings(src.Settings)
	case FieldTags:
		dst.Tags = cloneTags(src.Tags)
	case FieldColor:
		dst.Color = src.Color
	case FieldIcon:
		dst.Icon = src.Icon
	case FieldEnvironment:
		dst.Environment = src.Environment
	case FieldDescription:
		dst.Description = src.Description
	}
}

func copyFolderField(dst *ConnectionNode, a *FolderNode, f Field) {
	d := a.Defaults
	switch f {
	case FieldProtocolID:
		dst.ProtocolID = d.ProtocolID
	case FieldHost:
		dst.Host = d.Host
	case FieldPort:
		dst.Port = d.Port
	case FieldUsername:
		dst.Username = d.Username
	case FieldAuthMethod:
		dst.AuthMethod = d.AuthMethod
	case FieldCredentialRef:
		dst.CredentialRef = cloneRef(d.CredentialRef)
	case FieldSettings:
		dst.Settings = cloneSettings(d.Settings)
	case FieldTags:
		dst.Tags = cloneTags(d.Tags)
	case FieldColor:
		dst.Color = d.Color
	case FieldIcon:
		dst.Icon = d.Icon
	case FieldEnvironment:
		dst.Environment = d.Environment
	case FieldDescription:
		dst.Description = d.Description
	}
}

func zeroNodeField(n *ConnectionNode, f Field) {
	switch f {
	case FieldProtocolID:
		n.ProtocolID = ""
	case FieldHost:
		n.Host = ""
	case FieldPort:
		n.Port = 0
	case FieldUsername:
		n.Username = ""
	case FieldAuthMethod:
		n.AuthMethod = protocol.AuthMethod("")
	case FieldCredentialRef:
		n.CredentialRef = credential.Reference{}
	case FieldSettings:
		n.Settings = nil
	case FieldTags:
		n.Tags = nil
	case FieldColor:
		n.Color = ""
	case FieldIcon:
		n.Icon = ""
	case FieldEnvironment:
		n.Environment = ""
	case FieldDescription:
		n.Description = ""
	}
}

func isRefZero(r credential.Reference) bool {
	return r.ProviderID == "" && r.EntryID == "" && len(r.Hints) == 0
}

func cloneRef(r credential.Reference) credential.Reference {
	out := credential.Reference{ProviderID: r.ProviderID, EntryID: r.EntryID}
	if len(r.Hints) > 0 {
		out.Hints = make(map[string]string, len(r.Hints))
		for k, v := range r.Hints {
			out.Hints[k] = v
		}
	}
	return out
}

func cloneSettings(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneTags(t []string) []string {
	if len(t) == 0 {
		return nil
	}
	out := make([]string, len(t))
	copy(out, t)
	return out
}
