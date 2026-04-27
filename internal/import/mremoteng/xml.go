package mremoteng

import (
	"encoding/xml"
	"fmt"
	"io"
)

// assignRawAttr writes the attribute (by name) into the given rawConnection.
// Unknown names are stored in Unknown. Returns true if the name was known.
func assignRawAttr(r *rawConnection, name, value string) bool {
	switch name {
	case "Name":
		r.Name = value
	case "Type":
		r.Type = value
	case "Id":
		r.ID = value
	case "Descr":
		r.Descr = value
	case "Icon":
		r.Icon = value
	case "Panel":
		r.Panel = value
	case "Hostname":
		r.Hostname = value
	case "Protocol":
		r.Protocol = value
	case "PuttySession":
		r.PuttySession = value
	case "Port":
		r.Port = value
	case "Username":
		r.Username = value
	case "Domain":
		r.Domain = value
	case "Password":
		r.Password = value
	case "ConnectToConsole":
		r.ConnectToConsole = value
	case "UseConsoleSession":
		r.UseConsoleSession = value
	case "UseCredSsp":
		r.UseCredSsp = value
	case "RenderingEngine":
		r.RenderingEngine = value
	case "ICAEncryptionStrength":
		r.ICAEncryptionStrength = value
	case "RDPAuthenticationLevel":
		r.RDPAuthenticationLevel = value
	case "RDPMinutesIdleTimeout":
		r.RDPMinutesIdleTimeout = value
	case "RDPAlertIdleTimeout":
		r.RDPAlertIdleTimeout = value
	case "LoadBalanceInfo":
		r.LoadBalanceInfo = value
	case "Colors":
		r.Colors = value
	case "Resolution":
		r.Resolution = value
	case "AutomaticResize":
		r.AutomaticResize = value
	case "DisplayWallpaper":
		r.DisplayWallpaper = value
	case "DisplayThemes":
		r.DisplayThemes = value
	case "EnableFontSmoothing":
		r.EnableFontSmoothing = value
	case "EnableDesktopComposition":
		r.EnableDesktopComposition = value
	case "CacheBitmaps":
		r.CacheBitmaps = value
	case "RedirectKeys":
		r.RedirectKeys = value
	case "RedirectDiskDrives":
		r.RedirectDiskDrives = value
	case "RedirectPrinters":
		r.RedirectPrinters = value
	case "RedirectPorts":
		r.RedirectPorts = value
	case "RedirectSmartCards":
		r.RedirectSmartCards = value
	case "RedirectSound":
		r.RedirectSound = value
	case "SoundQuality":
		r.SoundQuality = value
	case "PreExtApp":
		r.PreExtApp = value
	case "PostExtApp":
		r.PostExtApp = value
	case "MacAddress":
		r.MacAddress = value
	case "UserField":
		r.UserField = value
	case "ExtApp":
		r.ExtApp = value
	case "VNCCompression":
		r.VNCCompression = value
	case "VNCEncoding":
		r.VNCEncoding = value
	case "VNCAuthMode":
		r.VNCAuthMode = value
	case "VNCProxyType":
		r.VNCProxyType = value
	case "VNCProxyIP":
		r.VNCProxyIP = value
	case "VNCProxyPort":
		r.VNCProxyPort = value
	case "VNCProxyUsername":
		r.VNCProxyUsername = value
	case "VNCProxyPassword":
		r.VNCProxyPassword = value
	case "VNCColors":
		r.VNCColors = value
	case "VNCSmartSizeMode":
		r.VNCSmartSizeMode = value
	case "VNCViewOnly":
		r.VNCViewOnly = value
	case "RDGatewayUsageMethod":
		r.RDGatewayUsageMethod = value
	case "RDGatewayHostname":
		r.RDGatewayHostname = value
	case "RDGatewayUseConnectionCredentials":
		r.RDGatewayUseConnectionCreds = value
	case "RDGatewayUsername":
		r.RDGatewayUsername = value
	case "RDGatewayPassword":
		r.RDGatewayPassword = value
	case "RDGatewayDomain":
		r.RDGatewayDomain = value
	case "InheritCacheBitmaps":
		r.InheritCacheBitmaps = value
	case "InheritColors":
		r.InheritColors = value
	case "InheritDescription":
		r.InheritDescription = value
	case "InheritDisplayThemes":
		r.InheritDisplayThemes = value
	case "InheritDisplayWallpaper":
		r.InheritDisplayWallpaper = value
	case "InheritEnableFontSmoothing":
		r.InheritEnableFontSmoothing = value
	case "InheritEnableDesktopComposition":
		r.InheritEnableDesktopComposition = value
	case "InheritDomain":
		r.InheritDomain = value
	case "InheritIcon":
		r.InheritIcon = value
	case "InheritPanel":
		r.InheritPanel = value
	case "InheritPassword":
		r.InheritPassword = value
	case "InheritPort":
		r.InheritPort = value
	case "InheritProtocol":
		r.InheritProtocol = value
	case "InheritPuttySession":
		r.InheritPuttySession = value
	case "InheritRDGatewayUsageMethod":
		r.InheritRDGatewayUsageMethod = value
	case "InheritRDGatewayHostname":
		r.InheritRDGatewayHostname = value
	case "InheritRDGatewayUseConnectionCredentials":
		r.InheritRDGatewayUseConnectionCredentials = value
	case "InheritRDGatewayUsername":
		r.InheritRDGatewayUsername = value
	case "InheritRDGatewayPassword":
		r.InheritRDGatewayPassword = value
	case "InheritRDGatewayDomain":
		r.InheritRDGatewayDomain = value
	case "InheritResolution":
		r.InheritResolution = value
	case "InheritAutomaticResize":
		r.InheritAutomaticResize = value
	case "InheritRedirectKeys":
		r.InheritRedirectKeys = value
	case "InheritRedirectDiskDrives":
		r.InheritRedirectDiskDrives = value
	case "InheritRedirectPrinters":
		r.InheritRedirectPrinters = value
	case "InheritRedirectPorts":
		r.InheritRedirectPorts = value
	case "InheritRedirectSmartCards":
		r.InheritRedirectSmartCards = value
	case "InheritRedirectSound":
		r.InheritRedirectSound = value
	case "InheritSoundQuality":
		r.InheritSoundQuality = value
	case "InheritUseConsoleSession":
		r.InheritUseConsoleSession = value
	case "InheritUseCredSsp":
		r.InheritUseCredSsp = value
	case "InheritRenderingEngine":
		r.InheritRenderingEngine = value
	case "InheritUsername":
		r.InheritUsername = value
	case "InheritICAEncryptionStrength":
		r.InheritICAEncryptionStrength = value
	case "InheritRDPAuthenticationLevel":
		r.InheritRDPAuthenticationLevel = value
	case "InheritLoadBalanceInfo":
		r.InheritLoadBalanceInfo = value
	case "InheritPreExtApp":
		r.InheritPreExtApp = value
	case "InheritPostExtApp":
		r.InheritPostExtApp = value
	case "InheritMacAddress":
		r.InheritMacAddress = value
	case "InheritUserField":
		r.InheritUserField = value
	case "InheritExtApp":
		r.InheritExtApp = value
	case "InheritVNCCompression":
		r.InheritVNCCompression = value
	case "InheritVNCEncoding":
		r.InheritVNCEncoding = value
	case "InheritVNCAuthMode":
		r.InheritVNCAuthMode = value
	case "InheritVNCProxyType":
		r.InheritVNCProxyType = value
	case "InheritVNCProxyIP":
		r.InheritVNCProxyIP = value
	case "InheritVNCProxyPort":
		r.InheritVNCProxyPort = value
	case "InheritVNCProxyUsername":
		r.InheritVNCProxyUsername = value
	case "InheritVNCProxyPassword":
		r.InheritVNCProxyPassword = value
	case "InheritVNCColors":
		r.InheritVNCColors = value
	case "InheritVNCSmartSizeMode":
		r.InheritVNCSmartSizeMode = value
	case "InheritVNCViewOnly":
		r.InheritVNCViewOnly = value
	case "InheritRDPMinutesIdleTimeout":
		r.InheritRDPMinutesIdleTimeout = value
	case "InheritRDPAlertIdleTimeout":
		r.InheritRDPAlertIdleTimeout = value
	default:
		if r.Unknown == nil {
			r.Unknown = make(map[string]string)
		}
		r.Unknown[name] = value
		return false
	}
	return true
}

// parseXML reads a mRemoteNG Connections document from r and returns the
// top-level rawConnection children (one per immediate child of <Connections>).
func parseXML(r io.Reader) ([]rawConnection, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	// Seek the root <Connections> element.
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, fmt.Errorf("mremoteng: empty document")
		}
		if err != nil {
			return nil, fmt.Errorf("mremoteng: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local != "Connections" {
			return nil, fmt.Errorf("mremoteng: root element is %q, expected Connections", se.Name.Local)
		}
		break
	}

	var roots []rawConnection
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("mremoteng: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "Node" {
				if err := dec.Skip(); err != nil {
					return nil, err
				}
				continue
			}
			node, err := decodeNode(dec, t)
			if err != nil {
				return nil, err
			}
			roots = append(roots, node)
		case xml.EndElement:
			if t.Name.Local == "Connections" {
				return roots, nil
			}
		}
	}
	return roots, nil
}

// decodeNode recursively decodes a <Node> and its descendants. The caller
// supplies the already-consumed StartElement.
func decodeNode(dec *xml.Decoder, start xml.StartElement) (rawConnection, error) {
	var node rawConnection
	for _, a := range start.Attr {
		assignRawAttr(&node, a.Name.Local, a.Value)
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return node, fmt.Errorf("mremoteng: decoding <Node Name=%q>: %w", node.Name, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "Node" {
				child, err := decodeNode(dec, t)
				if err != nil {
					return node, err
				}
				node.Children = append(node.Children, child)
			} else {
				if err := dec.Skip(); err != nil {
					return node, err
				}
			}
		case xml.EndElement:
			return node, nil
		}
	}
}
