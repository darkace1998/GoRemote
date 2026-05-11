package mremoteng

import (
	"os"
	"strings"
	"testing"

	"github.com/darkace1998/GoRemote/internal/domain"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// collectConnections walks the tree and returns a name -> *ConnectionNode map.
func collectConnections(t *testing.T, tr *domain.Tree) map[string]*domain.ConnectionNode {
	t.Helper()
	out := map[string]*domain.ConnectionNode{}
	_ = tr.Walk(func(n domain.Node) error {
		if c, ok := n.(*domain.ConnectionNode); ok {
			out[c.Name] = c
		}
		return nil
	})
	return out
}

func collectFolders(t *testing.T, tr *domain.Tree) map[string]*domain.FolderNode {
	t.Helper()
	out := map[string]*domain.FolderNode{}
	_ = tr.Walk(func(n domain.Node) error {
		if f, ok := n.(*domain.FolderNode); ok {
			out[f.Name] = f
		}
		return nil
	})
	return out
}

func TestImportXML_SimpleHierarchy(t *testing.T) {
	r, err := ImportXML(openFixture(t, "simple.xml"))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if r.Stats.Folders != 2 {
		t.Errorf("folders: want 2, got %d", r.Stats.Folders)
	}
	if r.Stats.Connections != 3 {
		t.Errorf("connections: want 3, got %d", r.Stats.Connections)
	}

	folders := collectFolders(t, r.Tree)
	for _, want := range []string{"Production", "Staging"} {
		if _, ok := folders[want]; !ok {
			t.Errorf("missing folder %q", want)
		}
	}

	conns := collectConnections(t, r.Tree)
	web1, ok := conns["web1"]
	if !ok {
		t.Fatalf("missing web1")
	}
	if web1.ProtocolID != "io.goremote.protocol.ssh" {
		t.Errorf("web1 protocol: %q", web1.ProtocolID)
	}
	if web1.Host != "web1.example.com" || web1.Port != 22 {
		t.Errorf("web1 host/port: %q %d", web1.Host, web1.Port)
	}

	win1 := conns["win1"]
	if win1 == nil || win1.ProtocolID != "io.goremote.protocol.rdp" {
		t.Errorf("win1 protocol: %+v", win1)
	}
	if v, _ := win1.Settings["display_themes"].(string); v != "True" {
		t.Errorf("win1 display_themes setting missing: %#v", win1.Settings)
	}

	vnc := conns["vnc-box"]
	if vnc == nil || vnc.ProtocolID != "io.goremote.protocol.vnc" {
		t.Errorf("vnc-box protocol: %+v", vnc)
	}

	// Verify parentage: web1 under Production.
	prod := folders["Production"]
	if prod == nil {
		t.Fatal("no Production folder")
	}
	if web1.ParentID != prod.ID {
		t.Errorf("web1 not under Production")
	}
}

func TestImportXML_InheritanceResolves(t *testing.T) {
	r, err := ImportXML(openFixture(t, "simple.xml"))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	conns := collectConnections(t, r.Tree)
	folders := collectFolders(t, r.Tree)

	web1 := conns["web1"]
	if !web1.Inheritance.Inherit[domain.FieldUsername] {
		t.Errorf("web1 should have FieldUsername marked inherited; got %+v", web1.Inheritance)
	}
	if web1.Inheritance.Inherit[domain.FieldPort] {
		t.Errorf("web1 FieldPort should NOT be inherited (InheritPort=False)")
	}

	win1 := conns["win1"]
	if !win1.Inheritance.Inherit[domain.FieldProtocolID] {
		t.Errorf("win1 should have FieldProtocolID inherited")
	}

	// Confirm domain.Resolve respects the profile: if we set the parent
	// folder's Username default, web1 should inherit it instead of using
	// its own "admin" value.
	prod := folders["Production"]
	prod.Defaults.Username = "root"
	ancestors, err := r.Tree.Ancestors(web1.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolved := web1.Inheritance.Resolve(web1, ancestors)
	if resolved.Username != "root" {
		t.Errorf("Resolve: Username want %q, got %q", "root", resolved.Username)
	}
	if resolved.Port != 22 {
		t.Errorf("Resolve: Port want 22, got %d", resolved.Port)
	}
}

func TestImportXML_UnknownProtocolAndAttributes(t *testing.T) {
	r, err := ImportXML(openFixture(t, "unknown_protocol.xml"))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if r.Stats.Connections != 1 {
		t.Fatalf("want 1 connection, got %d", r.Stats.Connections)
	}
	if r.Stats.ProtocolUnknown != 1 {
		t.Errorf("ProtocolUnknown: want 1, got %d", r.Stats.ProtocolUnknown)
	}
	if r.Stats.AttrUnknown < 2 {
		t.Errorf("AttrUnknown: want >=2, got %d", r.Stats.AttrUnknown)
	}

	var gotUnknownProto, gotUnknownAttr bool
	for _, w := range r.Warnings {
		if w.Code == CodeUnknownProtocol {
			gotUnknownProto = true
		}
		if w.Code == CodeUnknownAttribute {
			gotUnknownAttr = true
		}
	}
	if !gotUnknownProto {
		t.Error("missing unknown_protocol warning")
	}
	if !gotUnknownAttr {
		t.Error("missing unknown_attribute warning")
	}

	c := collectConnections(t, r.Tree)["citrix-farm"]
	if c == nil {
		t.Fatal("no citrix-farm connection")
	}
	if c.ProtocolID != "" {
		t.Errorf("unknown protocol should leave ProtocolID empty, got %q", c.ProtocolID)
	}
	if v, _ := c.Settings["legacy_protocol"].(string); v != "ICA" {
		t.Errorf("legacy_protocol: want %q, got %v", "ICA", c.Settings["legacy_protocol"])
	}
	if _, ok := c.Settings["legacy_attr_futureattribute"]; !ok {
		t.Errorf("unknown attr should round-trip under legacy_attr_*; got %#v", c.Settings)
	}
}

func TestImportXML_EncryptedPassword(t *testing.T) {
	r, err := ImportXML(openFixture(t, "simple.xml"))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	web1 := collectConnections(t, r.Tree)["web1"]
	if web1 == nil {
		t.Fatal("no web1")
	}
	blob, _ := web1.Settings["legacy_password_blob"].(string)
	if !strings.Contains(blob, "AES-GCM:") {
		t.Errorf("legacy_password_blob missing cipher text: %q", blob)
	}

	var gotEnc bool
	for _, w := range r.Warnings {
		if w.Code == CodeEncryptedPassword && strings.Contains(w.Path, "web1") {
			gotEnc = true
		}
	}
	if !gotEnc {
		t.Error("missing encrypted_password warning")
	}
}

func TestImportCSV_Equivalent(t *testing.T) {
	r, err := ImportCSV(openFixture(t, "simple.csv"))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if r.Stats.Connections != 3 {
		t.Fatalf("csv connections: want 3, got %d", r.Stats.Connections)
	}
	conns := collectConnections(t, r.Tree)
	for _, name := range []string{"web1", "win1", "vnc-box"} {
		if _, ok := conns[name]; !ok {
			t.Errorf("csv: missing connection %q", name)
		}
	}
	// Protocol mapping applied the same way.
	if conns["web1"].ProtocolID != "io.goremote.protocol.ssh" {
		t.Errorf("csv web1 proto: %q", conns["web1"].ProtocolID)
	}
	if conns["win1"].ProtocolID != "io.goremote.protocol.rdp" {
		t.Errorf("csv win1 proto: %q", conns["win1"].ProtocolID)
	}
	// Inheritance translated.
	if !conns["win1"].Inheritance.Inherit[domain.FieldProtocolID] {
		t.Errorf("csv win1 should inherit protocol")
	}
}

func TestImportXML_Empty(t *testing.T) {
	_, err := ImportXML(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty document")
	}
}

func TestImportXML_EmptyConnections(t *testing.T) {
	// Well-formed but empty — should succeed with zero result and no warnings.
	r, err := ImportXML(strings.NewReader(`<?xml version="1.0"?><Connections Name="x" ConfVersion="2.7"></Connections>`))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if r.Stats.Folders != 0 || r.Stats.Connections != 0 {
		t.Errorf("want zero stats, got %+v", r.Stats)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("want no warnings, got %+v", r.Warnings)
	}
}

func TestImportXML_UnsupportedInheritFlag(t *testing.T) {
	doc := `<?xml version="1.0"?><Connections Name="x" ConfVersion="2.7">
  <Node Name="h" Type="Connection" Hostname="h.example.com" Protocol="SSH2"
        InheritColors="True" InheritResolution="True"/>
</Connections>`
	r, err := ImportXML(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	if r.Stats.InheritUnsupported < 2 {
		t.Errorf("InheritUnsupported: want >=2, got %d", r.Stats.InheritUnsupported)
	}
	c := collectConnections(t, r.Tree)["h"]
	if c == nil {
		t.Fatal("missing connection")
	}
	if v, _ := c.Settings["legacy_inherit_colors"].(bool); !v {
		t.Errorf("legacy_inherit_colors missing: %#v", c.Settings)
	}
	if v, _ := c.Settings["legacy_inherit_resolution"].(bool); !v {
		t.Errorf("legacy_inherit_resolution missing: %#v", c.Settings)
	}
}

// TestImportCSV_BOM verifies that a UTF-8 BOM at the start of a CSV file does
// not corrupt the first column header, causing connections to have an empty Name.
func TestImportCSV_BOM(t *testing.T) {
	const bomCSV = "\xEF\xBB\xBFName,Type,Hostname,Protocol,Port,Username\n" +
		"myhost,Connection,myhost.example.com,SSH2,22,admin\n"
	r, err := ImportCSV(strings.NewReader(bomCSV))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if r.Stats.Connections != 1 {
		t.Fatalf("want 1 connection, got %d", r.Stats.Connections)
	}
	conns := collectConnections(t, r.Tree)
	if _, ok := conns["myhost"]; !ok {
		var ks []string
		for k := range conns {
			ks = append(ks, k)
		}
		t.Errorf("connection name not found after BOM strip; got keys: %v", ks)
	}
}

// TestImportXML_ContainerInheritance verifies that Container-level attributes
// are stored in FolderNode.Defaults and that children with Inherit* flags
// resolve to folder defaults via domain.InheritanceProfile.Resolve.
func TestImportXML_ContainerInheritance(t *testing.T) {
	r, err := ImportXML(openFixture(t, "container_inherit.xml"))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}

	folders := collectFolders(t, r.Tree)
	folder := folders["DevFolder"]
	if folder == nil {
		t.Fatal("DevFolder not found")
	}
	if folder.Defaults.Username != "devuser" {
		t.Errorf("Defaults.Username want %q, got %q", "devuser", folder.Defaults.Username)
	}
	if folder.Defaults.Host != "dev.example.com" {
		t.Errorf("Defaults.Host want %q, got %q", "dev.example.com", folder.Defaults.Host)
	}
	if folder.Defaults.ProtocolID != "io.goremote.protocol.ssh" {
		t.Errorf("Defaults.ProtocolID want %q, got %q", "io.goremote.protocol.ssh", folder.Defaults.ProtocolID)
	}
	if folder.Defaults.Port != 22 {
		t.Errorf("Defaults.Port want 22, got %d", folder.Defaults.Port)
	}

	conns := collectConnections(t, r.Tree)

	explicit := conns["child-explicit"]
	if explicit == nil {
		t.Fatal("child-explicit not found")
	}
	if explicit.Inheritance.Inherit[domain.FieldUsername] {
		t.Error("child-explicit should NOT inherit username")
	}

	child := conns["child-inherit"]
	if child == nil {
		t.Fatal("child-inherit not found")
	}
	if !child.Inheritance.Inherit[domain.FieldUsername] {
		t.Error("child-inherit should inherit username")
	}
	if !child.Inheritance.Inherit[domain.FieldPort] {
		t.Error("child-inherit should inherit port")
	}
	if !child.Inheritance.Inherit[domain.FieldProtocolID] {
		t.Error("child-inherit should inherit protocol")
	}

	ancestors, err := r.Tree.Ancestors(child.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	resolved := child.Inheritance.Resolve(child, ancestors)
	if resolved.Username != "devuser" {
		t.Errorf("resolved Username want %q, got %q", "devuser", resolved.Username)
	}
	if resolved.Port != 22 {
		t.Errorf("resolved Port want 22, got %d", resolved.Port)
	}
	if resolved.ProtocolID != "io.goremote.protocol.ssh" {
		t.Errorf("resolved ProtocolID want %q, got %q", "io.goremote.protocol.ssh", resolved.ProtocolID)
	}
}

// TestImportXML_EncryptedProxyPasswords verifies that VNCProxyPassword and
// RDGatewayPassword are treated as encrypted blobs (CodeEncryptedPassword)
// and not stored as plain protocol settings.
func TestImportXML_EncryptedProxyPasswords(t *testing.T) {
	const doc = `<?xml version="1.0"?><Connections Name="x" ConfVersion="2.7">
  <Node Name="vnc-proxy" Type="Connection" Hostname="h" Protocol="VNC"
        VNCProxyPassword="AES-GCM:dmNwcm94eQ==" />
  <Node Name="rdgw" Type="Connection" Hostname="h" Protocol="RDP"
        RDGatewayPassword="AES-GCM:cmRndA==" />
</Connections>`
	r, err := ImportXML(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ImportXML: %v", err)
	}
	conns := collectConnections(t, r.Tree)

	vnc := conns["vnc-proxy"]
	if vnc == nil {
		t.Fatal("missing vnc-proxy")
	}
	if _, ok := vnc.Settings["vnc_proxy_password"]; ok {
		t.Error("VNCProxyPassword must NOT be stored as plain vnc_proxy_password")
	}
	if blob, _ := vnc.Settings["legacy_vnc_proxy_password_blob"].(string); blob == "" {
		t.Errorf("legacy_vnc_proxy_password_blob missing; settings: %#v", vnc.Settings)
	}

	rdgw := conns["rdgw"]
	if rdgw == nil {
		t.Fatal("missing rdgw")
	}
	if _, ok := rdgw.Settings["rd_gateway_password"]; ok {
		t.Error("RDGatewayPassword must NOT be stored as plain rd_gateway_password")
	}
	if rdBlob, _ := rdgw.Settings["legacy_rd_gateway_password_blob"].(string); rdBlob == "" {
		t.Errorf("legacy_rd_gateway_password_blob missing; settings: %#v", rdgw.Settings)
	}

	var gotVNC, gotRD bool
	for _, w := range r.Warnings {
		if w.Code == CodeEncryptedPassword && w.Field == "VNCProxyPassword" {
			gotVNC = true
		}
		if w.Code == CodeEncryptedPassword && w.Field == "RDGatewayPassword" {
			gotRD = true
		}
	}
	if !gotVNC {
		t.Error("missing CodeEncryptedPassword warning for VNCProxyPassword")
	}
	if !gotRD {
		t.Error("missing CodeEncryptedPassword warning for RDGatewayPassword")
	}
}
