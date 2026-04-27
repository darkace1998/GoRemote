package mremoteng

// rawConnection is the neutral intermediate form produced by both the XML
// and the CSV parsers, and consumed by the mapper. Every field is captured
// as a string so the mapper can emit warnings about invalid values without
// aborting the whole import.
type rawConnection struct {
	// Structural.
	Name  string
	Type  string // "Container" | "Connection"
	ID    string // mRemoteNG-assigned GUID
	Descr string
	Icon  string
	Panel string

	// Connection fields.
	Hostname     string
	Protocol     string
	PuttySession string
	Port         string
	Username     string
	Domain       string
	Password     string // opaque (usually AES-GCM ciphertext, base64)

	// Per-protocol settings (kept as strings so we can round-trip them).
	ConnectToConsole            string
	UseConsoleSession           string
	UseCredSsp                  string
	RenderingEngine             string
	ICAEncryptionStrength       string
	RDPAuthenticationLevel      string
	RDPMinutesIdleTimeout       string
	RDPAlertIdleTimeout         string
	LoadBalanceInfo             string
	Colors                      string
	Resolution                  string
	AutomaticResize             string
	DisplayWallpaper            string
	DisplayThemes               string
	EnableFontSmoothing         string
	EnableDesktopComposition    string
	CacheBitmaps                string
	RedirectKeys                string
	RedirectDiskDrives          string
	RedirectPrinters            string
	RedirectPorts               string
	RedirectSmartCards          string
	RedirectSound               string
	SoundQuality                string
	PreExtApp                   string
	PostExtApp                  string
	MacAddress                  string
	UserField                   string
	ExtApp                      string
	VNCCompression              string
	VNCEncoding                 string
	VNCAuthMode                 string
	VNCProxyType                string
	VNCProxyIP                  string
	VNCProxyPort                string
	VNCProxyUsername            string
	VNCProxyPassword            string
	VNCColors                   string
	VNCSmartSizeMode            string
	VNCViewOnly                 string
	RDGatewayUsageMethod        string
	RDGatewayHostname           string
	RDGatewayUseConnectionCreds string
	RDGatewayUsername           string
	RDGatewayPassword           string
	RDGatewayDomain             string

	// Inheritance flags ("true" / "false"). A missing flag is treated as
	// "false" (i.e. explicit value on the node).
	InheritCacheBitmaps                      string
	InheritColors                            string
	InheritDescription                       string
	InheritDisplayThemes                     string
	InheritDisplayWallpaper                  string
	InheritEnableFontSmoothing               string
	InheritEnableDesktopComposition          string
	InheritDomain                            string
	InheritIcon                              string
	InheritPanel                             string
	InheritPassword                          string
	InheritPort                              string
	InheritProtocol                          string
	InheritPuttySession                      string
	InheritRDGatewayUsageMethod              string
	InheritRDGatewayHostname                 string
	InheritRDGatewayUseConnectionCredentials string
	InheritRDGatewayUsername                 string
	InheritRDGatewayPassword                 string
	InheritRDGatewayDomain                   string
	InheritResolution                        string
	InheritAutomaticResize                   string
	InheritRedirectKeys                      string
	InheritRedirectDiskDrives                string
	InheritRedirectPrinters                  string
	InheritRedirectPorts                     string
	InheritRedirectSmartCards                string
	InheritRedirectSound                     string
	InheritSoundQuality                      string
	InheritUseConsoleSession                 string
	InheritUseCredSsp                        string
	InheritRenderingEngine                   string
	InheritUsername                          string
	InheritICAEncryptionStrength             string
	InheritRDPAuthenticationLevel            string
	InheritLoadBalanceInfo                   string
	InheritPreExtApp                         string
	InheritPostExtApp                        string
	InheritMacAddress                        string
	InheritUserField                         string
	InheritExtApp                            string
	InheritVNCCompression                    string
	InheritVNCEncoding                       string
	InheritVNCAuthMode                       string
	InheritVNCProxyType                      string
	InheritVNCProxyIP                        string
	InheritVNCProxyPort                      string
	InheritVNCProxyUsername                  string
	InheritVNCProxyPassword                  string
	InheritVNCColors                         string
	InheritVNCSmartSizeMode                  string
	InheritVNCViewOnly                       string
	InheritRDPMinutesIdleTimeout             string
	InheritRDPAlertIdleTimeout               string

	// Unknown attributes encountered on the source element, preserved for
	// warning emission and for round-tripping. Keyed by the attribute name
	// exactly as it appeared on the wire.
	Unknown map[string]string

	// Children (XML only; always nil for CSV rows).
	Children []rawConnection
}

// knownAttrs enumerates every attribute name the importer recognises. A
// lowercase copy of the set is consulted by the unknown-attribute detection
// path so we can be case-insensitive against mRemoteNG variants.
var knownAttrs = map[string]struct{}{}

func init() {
	for _, n := range []string{
		// Structural.
		"Name", "Type", "Descr", "Icon", "Panel", "Id",
		// Connection fields.
		"Hostname", "Protocol", "PuttySession", "Port",
		"Username", "Domain", "Password",
		// Per-protocol settings.
		"ConnectToConsole", "UseConsoleSession", "UseCredSsp",
		"RenderingEngine", "ICAEncryptionStrength",
		"RDPAuthenticationLevel", "RDPMinutesIdleTimeout",
		"RDPAlertIdleTimeout", "LoadBalanceInfo",
		"Colors", "Resolution", "AutomaticResize",
		"DisplayWallpaper", "DisplayThemes",
		"EnableFontSmoothing", "EnableDesktopComposition", "CacheBitmaps",
		"RedirectKeys", "RedirectDiskDrives", "RedirectPrinters",
		"RedirectPorts", "RedirectSmartCards", "RedirectSound",
		"SoundQuality", "PreExtApp", "PostExtApp",
		"MacAddress", "UserField", "ExtApp",
		"VNCCompression", "VNCEncoding", "VNCAuthMode",
		"VNCProxyType", "VNCProxyIP", "VNCProxyPort",
		"VNCProxyUsername", "VNCProxyPassword",
		"VNCColors", "VNCSmartSizeMode", "VNCViewOnly",
		"RDGatewayUsageMethod", "RDGatewayHostname",
		"RDGatewayUseConnectionCredentials",
		"RDGatewayUsername", "RDGatewayPassword", "RDGatewayDomain",
		// Inheritance flags.
		"InheritCacheBitmaps", "InheritColors", "InheritDescription",
		"InheritDisplayThemes", "InheritDisplayWallpaper",
		"InheritEnableFontSmoothing", "InheritEnableDesktopComposition",
		"InheritDomain", "InheritIcon", "InheritPanel", "InheritPassword",
		"InheritPort", "InheritProtocol", "InheritPuttySession",
		"InheritRDGatewayUsageMethod", "InheritRDGatewayHostname",
		"InheritRDGatewayUseConnectionCredentials",
		"InheritRDGatewayUsername", "InheritRDGatewayPassword",
		"InheritRDGatewayDomain", "InheritResolution",
		"InheritAutomaticResize", "InheritRedirectKeys",
		"InheritRedirectDiskDrives", "InheritRedirectPrinters",
		"InheritRedirectPorts", "InheritRedirectSmartCards",
		"InheritRedirectSound", "InheritSoundQuality",
		"InheritUseConsoleSession", "InheritUseCredSsp",
		"InheritRenderingEngine", "InheritUsername",
		"InheritICAEncryptionStrength", "InheritRDPAuthenticationLevel",
		"InheritLoadBalanceInfo", "InheritPreExtApp", "InheritPostExtApp",
		"InheritMacAddress", "InheritUserField", "InheritExtApp",
		"InheritVNCCompression", "InheritVNCEncoding", "InheritVNCAuthMode",
		"InheritVNCProxyType", "InheritVNCProxyIP", "InheritVNCProxyPort",
		"InheritVNCProxyUsername", "InheritVNCProxyPassword",
		"InheritVNCColors", "InheritVNCSmartSizeMode",
		"InheritVNCViewOnly",
		"InheritRDPMinutesIdleTimeout", "InheritRDPAlertIdleTimeout",
	} {
		knownAttrs[n] = struct{}{}
	}
}
