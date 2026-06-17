# 📝 Update VNC and HTTP protocol documentation to reflect dropped Go-native implementation

**What:** Updated `PARITY.md`, `README.md`, `getting-started.md`, and `protocols.md` to reflect the dropped experimental status of the Pure-Go RFB (VNC) client and embedded WebView (HTTP).
**Why:** The architecture shifted away from native webview and pure-Go RFB engines (as noted in `TODO.md`), but user-facing docs still incorrectly marked them as experimental/planned Go-native modules.
**How:**
- Removed HTTP from the protocol scaffolding list in `README.md`.
- Dropped VNC from the native protocol highlights in `README.md`.
- Updated `PARITY.md` to show they are dropped as native modules and rely on external launchers.
- Amended the how-to guides (`protocols.md`, `getting-started.md`) to point users to the external system browser and `vncviewer` launchers.
**Testing/Verification:** Ran `git diff` to verify exact wordings were correct, then ran `make test` (with `GOTOOLCHAIN=auto`) to ensure documentation changes didn't trip any formatters or embedded doc tests.
