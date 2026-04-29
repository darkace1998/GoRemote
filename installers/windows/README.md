# Windows installer (WiX 4)

`goremote.wxs` builds an MSI for the desktop app on Windows. It is invoked
from `.github/workflows/release.yml` after the `goremote.exe` artifact has
been Authenticode-signed.

## Required release secrets / variables

Set these as GitHub Actions secrets on the repository before triggering
the `Release` workflow:

| Name                       | Purpose                                                         |
|----------------------------|-----------------------------------------------------------------|
| `GOREMOTE_RELEASE_KEY`     | Base64 Ed25519 private key for signing `update.json` manifest.  |
| `WINDOWS_CERT_PFX_BASE64`  | Base64 of the `.pfx` Authenticode certificate.                  |
| `WINDOWS_CERT_PASSWORD`    | Password protecting the `.pfx`.                                 |
| `MACOS_CERT_P12_BASE64`    | Base64 of the Apple Developer ID `.p12`. (deferred)             |
| `MACOS_CERT_PASSWORD`      | Password for the `.p12`. (deferred)                             |
| `MACOS_NOTARY_USER`        | App-Store-Connect Apple ID for `notarytool`. (deferred)         |
| `MACOS_NOTARY_TEAM_ID`     | Apple Developer team ID. (deferred)                             |
| `MACOS_NOTARY_PASSWORD`    | App-specific password for `notarytool`. (deferred)              |

## Stable identifiers

- **MSI UpgradeCode**: replace the `PUT-A-STABLE-GUID-HERE` placeholder in
  the workflow's `-d UpgradeCode=` argument with a real GUID and never
  change it. Changing it breaks in-place upgrade for existing installs.
- **Release workflow behaviour without UpgradeCode**: if
  `WINDOWS_UPGRADE_CODE` is not configured, the release workflow now skips MSI
  creation and still publishes the signed Windows `.exe` so the rest of the
  release can complete.
- **Ed25519 release key**: the *public* half is shipped to users as the
  `Settings.AutoUpdatePublicKey` value (stored in their profile). Rotate
  by issuing a new app version that ships an updated default public key
  embedded in `app/settings/defaults.go`.

## Local manual build

```powershell
$env:Version = "0.9.0"
wix build goremote.wxs -d Version=$env:Version -d SourceExe=path\to\goremote.exe `
    -d UpgradeCode=PUT-A-STABLE-GUID-HERE -d LicenseRtf=path\to\license.rtf `
    -o goremote-$env:Version.msi
signtool sign /tr http://timestamp.digicert.com /td sha256 /fd sha256 `
    /f cert.pfx /p $env:CertPassword goremote-$env:Version.msi
```
