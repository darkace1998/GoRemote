package mremoteng

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/goremote/goremote/internal/domain"
)

// Warning severities.
const (
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

// Warning codes.
const (
	CodeUnknownProtocol    = "unknown_protocol"
	CodeUnknownAttribute   = "unknown_attribute"
	CodeInheritUnsupported = "inherit_unsupported"
	CodeEncryptedPassword  = "encrypted_password"
	CodeProtocolSetting    = "protocol_setting"
	CodeInvalidPort        = "invalid_port"
	CodeExternalProtocol   = "external_protocol"
)

// Warning is a single non-fatal issue surfaced by the importer.
type Warning struct {
	// Severity is "info", "warn", or "error".
	Severity string
	// Code is a stable machine-readable identifier.
	Code string
	// Message is a human-readable description.
	Message string
	// Path is a "/"-delimited name path to the offending node, or empty.
	Path string
	// Field names the offending attribute/setting, when applicable.
	Field string
}

// Stats summarises what the importer processed.
type Stats struct {
	Folders            int
	Connections        int
	ProtocolUnknown    int
	AttrUnknown        int
	InheritUnsupported int
}

// Result is the outcome of an import.
type Result struct {
	Tree     *domain.Tree
	Warnings []Warning
	Stats    Stats
}

// ImportXML parses a mRemoteNG Confcons.xml document and returns a
// domain.Tree populated with the folders and connections it describes.
//
// The function never returns an error for recoverable conditions (unknown
// protocols, unknown attributes, unsupported inheritance flags, encrypted
// passwords). Those are surfaced on Result.Warnings. An error is returned
// only if the document is malformed or empty.
func ImportXML(r io.Reader) (*Result, error) {
	roots, err := parseXML(r)
	if err != nil {
		return nil, err
	}
	imp := newImporter()
	for _, n := range roots {
		imp.walk(&n, domain.NilID, "")
	}
	return imp.finish(), nil
}

// ImportCSV parses a mRemoteNG CSV export. Every row becomes a
// ConnectionNode attached to the tree root; CSV has no folder hierarchy.
func ImportCSV(r io.Reader) (*Result, error) {
	rows, err := parseCSV(r)
	if err != nil {
		return nil, err
	}
	imp := newImporter()
	for i := range rows {
		imp.addConnection(&rows[i], domain.NilID, "")
	}
	return imp.finish(), nil
}

type importer struct {
	tree     *domain.Tree
	warnings []Warning
	stats    Stats
}

func newImporter() *importer {
	return &importer{tree: domain.NewTree()}
}

func (i *importer) finish() *Result {
	return &Result{Tree: i.tree, Warnings: i.warnings, Stats: i.stats}
}

func (i *importer) warn(sev, code, msg, path, field string) {
	i.warnings = append(i.warnings, Warning{
		Severity: sev, Code: code, Message: msg, Path: path, Field: field,
	})
}

// walk processes an XML node recursively.
func (i *importer) walk(n *rawConnection, parentID domain.ID, parentPath string) {
	path := joinPath(parentPath, n.Name)
	switch strings.ToLower(strings.TrimSpace(n.Type)) {
	case "container":
		id := i.addFolder(n, parentID, path)
		for k := range n.Children {
			i.walk(&n.Children[k], id, path)
		}
	case "", "connection":
		if n.Type == "" && len(n.Children) > 0 {
			// No explicit type, but has children — treat as a container.
			id := i.addFolder(n, parentID, path)
			for k := range n.Children {
				i.walk(&n.Children[k], id, path)
			}
			return
		}
		i.addConnection(n, parentID, path)
	default:
		i.warn(SeverityWarn, "unknown_node_type",
			fmt.Sprintf("unknown node type %q; skipping", n.Type), path, "Type")
	}
}

func (i *importer) addFolder(n *rawConnection, parentID domain.ID, path string) domain.ID {
	f := &domain.FolderNode{
		ID:          domain.NewID(),
		ParentID:    parentID,
		Name:        n.Name,
		Description: n.Descr,
		Icon:        n.Icon,
	}
	if n.ID != "" {
		f.Defaults.Settings = map[string]any{"legacy_id": n.ID}
	}
	if err := i.tree.AddFolder(f); err != nil {
		i.warn(SeverityError, "tree_add_folder",
			fmt.Sprintf("failed to add folder: %v", err), path, "")
		return domain.NilID
	}
	i.stats.Folders++
	// Fold unknown attributes into a warning trail.
	i.emitUnknownAttrWarnings(n, path)
	return f.ID
}

func (i *importer) addConnection(n *rawConnection, parentID domain.ID, path string) {
	c := &domain.ConnectionNode{
		ID:          domain.NewID(),
		ParentID:    parentID,
		Name:        n.Name,
		Host:        n.Hostname,
		Username:    n.Username,
		Description: n.Descr,
		Icon:        n.Icon,
		Environment: n.Panel,
		Settings:    map[string]any{},
	}

	if n.ID != "" {
		c.Settings["legacy_id"] = n.ID
	}

	// Protocol mapping.
	if n.Protocol != "" {
		if id, ok := mapProtocol(n.Protocol); ok {
			c.ProtocolID = id
			if id == "io.goremote.protocol.external" {
				i.warn(SeverityWarn, CodeExternalProtocol,
					fmt.Sprintf("protocol %q mapped to external tool; goremote does not ship a built-in external-tool runner yet", n.Protocol),
					path, "Protocol")
			}
		} else {
			c.Settings["legacy_protocol"] = n.Protocol
			i.stats.ProtocolUnknown++
			i.warn(SeverityWarn, CodeUnknownProtocol,
				fmt.Sprintf("unknown protocol %q; preserved as legacy_protocol", n.Protocol),
				path, "Protocol")
		}
	}

	// Port parsing.
	if n.Port != "" {
		p := parsePort(n.Port)
		if p == 0 {
			i.warn(SeverityWarn, CodeInvalidPort,
				fmt.Sprintf("invalid port %q; ignored", n.Port), path, "Port")
		}
		c.Port = p
	}

	// Password (encrypted) handling.
	if n.Password != "" {
		c.Settings["legacy_password_blob"] = n.Password
		i.warn(SeverityWarn, CodeEncryptedPassword,
			"mRemoteNG password blob preserved as legacy_password_blob; encrypted-password import is not yet supported — re-enter the credential or attach a credential reference",
			path, "Password")
	}

	// Per-protocol settings: preserve verbatim, warn informationally.
	for _, ps := range perProtocolSettings {
		v := perProtocolValue(n, ps.attr)
		if v == "" {
			continue
		}
		c.Settings[ps.key] = v
		i.warn(SeverityInfo, CodeProtocolSetting,
			fmt.Sprintf("legacy setting %q preserved; goremote's protocol plugin may not honour it yet", ps.attr),
			path, ps.attr)
	}

	// Inheritance profile translation.
	i.applyInheritance(n, c, path)

	// Domain is a slightly special case: it's both a per-protocol setting
	// (RDP) and a concept some users map onto Username. We preserved it as
	// a setting above. No additional work required here.

	// Unknown attributes: warn + preserve under "legacy_attr_<lowercased>".
	if len(n.Unknown) > 0 {
		names := make([]string, 0, len(n.Unknown))
		for k := range n.Unknown {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			val := n.Unknown[name]
			c.Settings["legacy_attr_"+strings.ToLower(name)] = val
			i.stats.AttrUnknown++
			i.warn(SeverityInfo, CodeUnknownAttribute,
				fmt.Sprintf("unknown attribute %q preserved as legacy_attr_%s",
					name, strings.ToLower(name)),
				path, name)
		}
	}

	if len(c.Settings) == 0 {
		c.Settings = nil
	}

	if err := i.tree.AddConnection(c); err != nil {
		i.warn(SeverityError, "tree_add_connection",
			fmt.Sprintf("failed to add connection: %v", err), path, "")
		return
	}
	i.stats.Connections++
}

// applyInheritance walks every known InheritXxx attribute and, when it is
// set to "true", either marks the corresponding domain.Field as inherited
// on the ConnectionNode, or (for attributes without a domain equivalent)
// records the flag under Settings and emits a warning.
func (i *importer) applyInheritance(n *rawConnection, c *domain.ConnectionNode, path string) {
	for _, name := range allInheritanceAttrs {
		v := inheritanceValue(n, name)
		b, known := parseBool(v)
		if !known {
			continue
		}
		if !b {
			continue
		}
		if f, ok := inheritanceFieldMap[name]; ok {
			c.Inheritance.SetInherit(f)
			continue
		}
		// No native equivalent: preserve the flag, surface a warning.
		key := "legacy_inherit_" + strings.TrimPrefix(name, "Inherit")
		c.Settings[strings.ToLower(key)] = true
		i.stats.InheritUnsupported++
		i.warn(SeverityInfo, CodeInheritUnsupported,
			fmt.Sprintf("mRemoteNG inheritance flag %q has no native goremote equivalent; preserved as %s", name, strings.ToLower(key)),
			path, name)
	}
}

// emitUnknownAttrWarnings records every unknown attribute captured on the
// raw folder node as a Warning. Folder nodes have no generic Settings bag
// so the values are not copied, only surfaced.
func (i *importer) emitUnknownAttrWarnings(n *rawConnection, path string) {
	if len(n.Unknown) == 0 {
		return
	}
	names := make([]string, 0, len(n.Unknown))
	for k := range n.Unknown {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		i.stats.AttrUnknown++
		i.warn(SeverityInfo, CodeUnknownAttribute,
			fmt.Sprintf("unknown attribute %q on folder (dropped)", name), path, name)
	}
}

func joinPath(parent, name string) string {
	if parent == "" {
		return "/" + name
	}
	return parent + "/" + name
}
