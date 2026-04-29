package mremoteng

import (
	"strconv"
	"strings"

	"github.com/darkace1998/GoRemote/internal/domain"
)

// protocolMap maps a mRemoteNG Protocol attribute value to a goremote
// protocol ID. Unmapped values are handled by mapProtocol.
var protocolMap = map[string]string{
	"ssh2":         "io.goremote.protocol.ssh",
	"ssh1":         "io.goremote.protocol.ssh",
	"ssh":          "io.goremote.protocol.ssh",
	"telnet":       "io.goremote.protocol.telnet",
	"rdp":          "io.goremote.protocol.rdp",
	"vnc":          "io.goremote.protocol.vnc",
	"rlogin":       "io.goremote.protocol.rlogin",
	"raw":          "io.goremote.protocol.rawsocket",
	"rawsocket":    "io.goremote.protocol.rawsocket",
	"http":         "io.goremote.protocol.http",
	"https":        "io.goremote.protocol.http",
	"tn5250":       "io.goremote.protocol.tn5250",
	"powershell":   "io.goremote.protocol.powershell",
	"intapp":       "io.goremote.protocol.external",
	"externaltool": "io.goremote.protocol.external",
}

// mapProtocol returns (protocolID, mapped). mapped==false means the caller
// should preserve the original string under Settings["legacy_protocol"] and
// emit a warning.
func mapProtocol(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	id, ok := protocolMap[strings.ToLower(raw)]
	return id, ok
}

// inheritanceFieldMap translates an mRemoteNG Inherit* attribute name to the
// corresponding domain.Field. Entries missing from this map have no direct
// domain equivalent and are preserved verbatim under Settings.
var inheritanceFieldMap = map[string]domain.Field{
	"InheritProtocol":    domain.FieldProtocolID,
	"InheritPort":        domain.FieldPort,
	"InheritUsername":    domain.FieldUsername,
	"InheritPassword":    domain.FieldCredentialRef,
	"InheritDescription": domain.FieldDescription,
	"InheritIcon":        domain.FieldIcon,
	"InheritPanel":       domain.FieldEnvironment,
}

// allInheritanceAttrs is the complete list of mRemoteNG inheritance
// attribute names so the mapper can surface unsupported flags as warnings
// deterministically.
var allInheritanceAttrs = []string{
	"InheritCacheBitmaps",
	"InheritColors",
	"InheritDescription",
	"InheritDisplayThemes",
	"InheritDisplayWallpaper",
	"InheritEnableFontSmoothing",
	"InheritEnableDesktopComposition",
	"InheritDomain",
	"InheritIcon",
	"InheritPanel",
	"InheritPassword",
	"InheritPort",
	"InheritProtocol",
	"InheritPuttySession",
	"InheritRDGatewayUsageMethod",
	"InheritRDGatewayHostname",
	"InheritRDGatewayUseConnectionCredentials",
	"InheritRDGatewayUsername",
	"InheritRDGatewayPassword",
	"InheritRDGatewayDomain",
	"InheritResolution",
	"InheritAutomaticResize",
	"InheritRedirectKeys",
	"InheritRedirectDiskDrives",
	"InheritRedirectPrinters",
	"InheritRedirectPorts",
	"InheritRedirectSmartCards",
	"InheritRedirectSound",
	"InheritSoundQuality",
	"InheritUseConsoleSession",
	"InheritUseCredSsp",
	"InheritRenderingEngine",
	"InheritUsername",
	"InheritICAEncryptionStrength",
	"InheritRDPAuthenticationLevel",
	"InheritLoadBalanceInfo",
	"InheritPreExtApp",
	"InheritPostExtApp",
	"InheritMacAddress",
	"InheritUserField",
	"InheritExtApp",
	"InheritVNCCompression",
	"InheritVNCEncoding",
	"InheritVNCAuthMode",
	"InheritVNCProxyType",
	"InheritVNCProxyIP",
	"InheritVNCProxyPort",
	"InheritVNCProxyUsername",
	"InheritVNCProxyPassword",
	"InheritVNCColors",
	"InheritVNCSmartSizeMode",
	"InheritVNCViewOnly",
	"InheritRDPMinutesIdleTimeout",
	"InheritRDPAlertIdleTimeout",
}

// inheritanceValue returns the string value on r for a given Inherit* name.
func inheritanceValue(r *rawConnection, name string) string {
	switch name {
	case "InheritCacheBitmaps":
		return r.InheritCacheBitmaps
	case "InheritColors":
		return r.InheritColors
	case "InheritDescription":
		return r.InheritDescription
	case "InheritDisplayThemes":
		return r.InheritDisplayThemes
	case "InheritDisplayWallpaper":
		return r.InheritDisplayWallpaper
	case "InheritEnableFontSmoothing":
		return r.InheritEnableFontSmoothing
	case "InheritEnableDesktopComposition":
		return r.InheritEnableDesktopComposition
	case "InheritDomain":
		return r.InheritDomain
	case "InheritIcon":
		return r.InheritIcon
	case "InheritPanel":
		return r.InheritPanel
	case "InheritPassword":
		return r.InheritPassword
	case "InheritPort":
		return r.InheritPort
	case "InheritProtocol":
		return r.InheritProtocol
	case "InheritPuttySession":
		return r.InheritPuttySession
	case "InheritRDGatewayUsageMethod":
		return r.InheritRDGatewayUsageMethod
	case "InheritRDGatewayHostname":
		return r.InheritRDGatewayHostname
	case "InheritRDGatewayUseConnectionCredentials":
		return r.InheritRDGatewayUseConnectionCredentials
	case "InheritRDGatewayUsername":
		return r.InheritRDGatewayUsername
	case "InheritRDGatewayPassword":
		return r.InheritRDGatewayPassword
	case "InheritRDGatewayDomain":
		return r.InheritRDGatewayDomain
	case "InheritResolution":
		return r.InheritResolution
	case "InheritAutomaticResize":
		return r.InheritAutomaticResize
	case "InheritRedirectKeys":
		return r.InheritRedirectKeys
	case "InheritRedirectDiskDrives":
		return r.InheritRedirectDiskDrives
	case "InheritRedirectPrinters":
		return r.InheritRedirectPrinters
	case "InheritRedirectPorts":
		return r.InheritRedirectPorts
	case "InheritRedirectSmartCards":
		return r.InheritRedirectSmartCards
	case "InheritRedirectSound":
		return r.InheritRedirectSound
	case "InheritSoundQuality":
		return r.InheritSoundQuality
	case "InheritUseConsoleSession":
		return r.InheritUseConsoleSession
	case "InheritUseCredSsp":
		return r.InheritUseCredSsp
	case "InheritRenderingEngine":
		return r.InheritRenderingEngine
	case "InheritUsername":
		return r.InheritUsername
	case "InheritICAEncryptionStrength":
		return r.InheritICAEncryptionStrength
	case "InheritRDPAuthenticationLevel":
		return r.InheritRDPAuthenticationLevel
	case "InheritLoadBalanceInfo":
		return r.InheritLoadBalanceInfo
	case "InheritPreExtApp":
		return r.InheritPreExtApp
	case "InheritPostExtApp":
		return r.InheritPostExtApp
	case "InheritMacAddress":
		return r.InheritMacAddress
	case "InheritUserField":
		return r.InheritUserField
	case "InheritExtApp":
		return r.InheritExtApp
	case "InheritVNCCompression":
		return r.InheritVNCCompression
	case "InheritVNCEncoding":
		return r.InheritVNCEncoding
	case "InheritVNCAuthMode":
		return r.InheritVNCAuthMode
	case "InheritVNCProxyType":
		return r.InheritVNCProxyType
	case "InheritVNCProxyIP":
		return r.InheritVNCProxyIP
	case "InheritVNCProxyPort":
		return r.InheritVNCProxyPort
	case "InheritVNCProxyUsername":
		return r.InheritVNCProxyUsername
	case "InheritVNCProxyPassword":
		return r.InheritVNCProxyPassword
	case "InheritVNCColors":
		return r.InheritVNCColors
	case "InheritVNCSmartSizeMode":
		return r.InheritVNCSmartSizeMode
	case "InheritVNCViewOnly":
		return r.InheritVNCViewOnly
	case "InheritRDPMinutesIdleTimeout":
		return r.InheritRDPMinutesIdleTimeout
	case "InheritRDPAlertIdleTimeout":
		return r.InheritRDPAlertIdleTimeout
	}
	return ""
}

// perProtocolSettings lists the (XML attribute name, settings-map key)
// pairs we capture verbatim. Keys are lowercased so they round-trip
// deterministically.
var perProtocolSettings = []struct {
	attr string
	key  string
}{
	{"ConnectToConsole", "connect_to_console"},
	{"UseConsoleSession", "use_console_session"},
	{"UseCredSsp", "use_credssp"},
	{"RenderingEngine", "rendering_engine"},
	{"ICAEncryptionStrength", "ica_encryption_strength"},
	{"RDPAuthenticationLevel", "rdp_authentication_level"},
	{"RDPMinutesIdleTimeout", "rdp_minutes_idle_timeout"},
	{"RDPAlertIdleTimeout", "rdp_alert_idle_timeout"},
	{"LoadBalanceInfo", "load_balance_info"},
	{"Colors", "colors"},
	{"Resolution", "resolution"},
	{"AutomaticResize", "automatic_resize"},
	{"DisplayWallpaper", "display_wallpaper"},
	{"DisplayThemes", "display_themes"},
	{"EnableFontSmoothing", "enable_font_smoothing"},
	{"EnableDesktopComposition", "enable_desktop_composition"},
	{"CacheBitmaps", "cache_bitmaps"},
	{"RedirectKeys", "redirect_keys"},
	{"RedirectDiskDrives", "redirect_disk_drives"},
	{"RedirectPrinters", "redirect_printers"},
	{"RedirectPorts", "redirect_ports"},
	{"RedirectSmartCards", "redirect_smart_cards"},
	{"RedirectSound", "redirect_sound"},
	{"SoundQuality", "sound_quality"},
	{"PreExtApp", "pre_ext_app"},
	{"PostExtApp", "post_ext_app"},
	{"MacAddress", "mac_address"},
	{"UserField", "user_field"},
	{"ExtApp", "ext_app"},
	{"VNCCompression", "vnc_compression"},
	{"VNCEncoding", "vnc_encoding"},
	{"VNCAuthMode", "vnc_auth_mode"},
	{"VNCProxyType", "vnc_proxy_type"},
	{"VNCProxyIP", "vnc_proxy_ip"},
	{"VNCProxyPort", "vnc_proxy_port"},
	{"VNCProxyUsername", "vnc_proxy_username"},
	{"VNCProxyPassword", "vnc_proxy_password"},
	{"VNCColors", "vnc_colors"},
	{"VNCSmartSizeMode", "vnc_smart_size_mode"},
	{"VNCViewOnly", "vnc_view_only"},
	{"RDGatewayUsageMethod", "rd_gateway_usage_method"},
	{"RDGatewayHostname", "rd_gateway_hostname"},
	{"RDGatewayUseConnectionCredentials", "rd_gateway_use_connection_credentials"},
	{"RDGatewayUsername", "rd_gateway_username"},
	{"RDGatewayPassword", "rd_gateway_password"},
	{"RDGatewayDomain", "rd_gateway_domain"},
	{"PuttySession", "putty_session"},
	{"Domain", "domain"},
}

func perProtocolValue(r *rawConnection, attr string) string {
	switch attr {
	case "ConnectToConsole":
		return r.ConnectToConsole
	case "UseConsoleSession":
		return r.UseConsoleSession
	case "UseCredSsp":
		return r.UseCredSsp
	case "RenderingEngine":
		return r.RenderingEngine
	case "ICAEncryptionStrength":
		return r.ICAEncryptionStrength
	case "RDPAuthenticationLevel":
		return r.RDPAuthenticationLevel
	case "RDPMinutesIdleTimeout":
		return r.RDPMinutesIdleTimeout
	case "RDPAlertIdleTimeout":
		return r.RDPAlertIdleTimeout
	case "LoadBalanceInfo":
		return r.LoadBalanceInfo
	case "Colors":
		return r.Colors
	case "Resolution":
		return r.Resolution
	case "AutomaticResize":
		return r.AutomaticResize
	case "DisplayWallpaper":
		return r.DisplayWallpaper
	case "DisplayThemes":
		return r.DisplayThemes
	case "EnableFontSmoothing":
		return r.EnableFontSmoothing
	case "EnableDesktopComposition":
		return r.EnableDesktopComposition
	case "CacheBitmaps":
		return r.CacheBitmaps
	case "RedirectKeys":
		return r.RedirectKeys
	case "RedirectDiskDrives":
		return r.RedirectDiskDrives
	case "RedirectPrinters":
		return r.RedirectPrinters
	case "RedirectPorts":
		return r.RedirectPorts
	case "RedirectSmartCards":
		return r.RedirectSmartCards
	case "RedirectSound":
		return r.RedirectSound
	case "SoundQuality":
		return r.SoundQuality
	case "PreExtApp":
		return r.PreExtApp
	case "PostExtApp":
		return r.PostExtApp
	case "MacAddress":
		return r.MacAddress
	case "UserField":
		return r.UserField
	case "ExtApp":
		return r.ExtApp
	case "VNCCompression":
		return r.VNCCompression
	case "VNCEncoding":
		return r.VNCEncoding
	case "VNCAuthMode":
		return r.VNCAuthMode
	case "VNCProxyType":
		return r.VNCProxyType
	case "VNCProxyIP":
		return r.VNCProxyIP
	case "VNCProxyPort":
		return r.VNCProxyPort
	case "VNCProxyUsername":
		return r.VNCProxyUsername
	case "VNCProxyPassword":
		return r.VNCProxyPassword
	case "VNCColors":
		return r.VNCColors
	case "VNCSmartSizeMode":
		return r.VNCSmartSizeMode
	case "VNCViewOnly":
		return r.VNCViewOnly
	case "RDGatewayUsageMethod":
		return r.RDGatewayUsageMethod
	case "RDGatewayHostname":
		return r.RDGatewayHostname
	case "RDGatewayUseConnectionCredentials":
		return r.RDGatewayUseConnectionCreds
	case "RDGatewayUsername":
		return r.RDGatewayUsername
	case "RDGatewayPassword":
		return r.RDGatewayPassword
	case "RDGatewayDomain":
		return r.RDGatewayDomain
	case "PuttySession":
		return r.PuttySession
	case "Domain":
		return r.Domain
	}
	return ""
}

// parseBool parses the mRemoteNG "True"/"False"/"true"/"false" convention.
// Returns (value, known). known is false for empty or unrecognised inputs.
func parseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	}
	return false, false
}

// parsePort parses a port value, returning 0 on empty or unparseable.
func parsePort(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 65535 {
		return 0
	}
	return n
}
