import { useCallback, useEffect, useMemo, useState } from "react";
import {
  defaultSettings,
  settingsAPI,
  validate,
  type Settings,
  type Theme,
} from "../../api/settings";

const THEMES: Theme[] = ["system", "light", "dark"];

function applyThemeAttr(theme: Theme): void {
  if (typeof document === "undefined") return;
  const effective =
    theme === "system"
      ? typeof window !== "undefined" &&
        window.matchMedia?.("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : theme;
  document.documentElement.setAttribute("data-theme", effective);
  // Also mirror onto <body> so existing CSS keyed off body[data-theme=...]
  // (see theme/theme.css) keeps working.
  if (document.body) {
    document.body.setAttribute("data-theme", effective);
  }
}

interface SettingsViewProps {
  /** Optional close handler if the view is rendered as a modal/panel. */
  onClose?: () => void;
}

export function SettingsView({ onClose }: SettingsViewProps) {
  const api = settingsAPI();

  const [saved, setSaved] = useState<Settings>(defaultSettings());
  const [draft, setDraft] = useState<Settings>(defaultSettings());
  const [loading, setLoading] = useState<boolean>(true);
  const [saving, setSaving] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  // Initial load.
  useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .get()
      .then((s) => {
        if (!alive) return;
        setSaved(s);
        setDraft(s);
        applyThemeAttr(s.theme);
      })
      .catch((e: unknown) => {
        if (!alive) return;
        setError(`Failed to load settings: ${String(e)}`);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [api]);

  // Live theme preview as the user picks one.
  useEffect(() => {
    applyThemeAttr(draft.theme);
  }, [draft.theme]);

  const dirty = useMemo(
    () => !shallowSettingsEqual(draft, saved),
    [draft, saved],
  );

  const clientErrors = useMemo(() => validate(draft), [draft]);

  const onSave = useCallback(async () => {
    setError(null);
    if (clientErrors.length > 0) {
      setError(clientErrors.join("; "));
      return;
    }
    setSaving(true);
    try {
      const persisted = await api.update(draft);
      setSaved(persisted);
      setDraft(persisted);
      applyThemeAttr(persisted.theme);
    } catch (e: unknown) {
      setError(String(e instanceof Error ? e.message : e));
    } finally {
      setSaving(false);
    }
  }, [api, clientErrors, draft]);

  const onRevert = useCallback(() => {
    setDraft(saved);
    applyThemeAttr(saved.theme);
    setError(null);
  }, [saved]);

  const update = useCallback(<K extends keyof Settings>(key: K, value: Settings[K]) => {
    setDraft((d) => ({ ...d, [key]: value }));
  }, []);

  return (
    <section
      className="settings-view"
      aria-labelledby="settings-heading"
      style={{
        padding: 16,
        maxWidth: 640,
        display: "flex",
        flexDirection: "column",
        gap: 12,
        color: "var(--fg)",
      }}
    >
      <header style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <h2 id="settings-heading" style={{ margin: 0 }}>
          Settings
        </h2>
        <div style={{ flex: 1 }} />
        {onClose && (
          <button onClick={onClose} aria-label="Close settings">
            Close
          </button>
        )}
      </header>

      {loading ? (
        <p style={{ color: "var(--fg-muted)" }}>Loading…</p>
      ) : (
        <>
          {error && (
            <div
              role="alert"
              data-testid="settings-error"
              style={{
                background: "var(--bg-elevated, #2a1a1a)",
                color: "var(--danger, #e06c75)",
                border: "1px solid var(--danger, #e06c75)",
                padding: 8,
                borderRadius: 4,
                fontSize: 13,
              }}
            >
              {error}
            </div>
          )}

          <fieldset style={fieldset}>
            <legend>Appearance</legend>
            <label style={row}>
              <span style={lbl}>Theme</span>
              <select
                aria-label="Theme"
                value={draft.theme}
                onChange={(e) => update("theme", e.target.value as Theme)}
              >
                {THEMES.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </label>
            <label style={row}>
              <span style={lbl}>Font family</span>
              <input
                aria-label="Font family"
                type="text"
                placeholder="(platform default)"
                value={draft.fontFamily}
                onChange={(e) => update("fontFamily", e.target.value)}
              />
            </label>
            <label style={row}>
              <span style={lbl}>Font size (px)</span>
              <input
                aria-label="Font size"
                type="number"
                min={8}
                max={72}
                value={draft.fontSizePx}
                onChange={(e) =>
                  update("fontSizePx", Number(e.target.value) || 0)
                }
              />
            </label>
          </fieldset>

          <fieldset style={fieldset}>
            <legend>Sessions</legend>
            <label style={row}>
              <input
                aria-label="Confirm on close"
                type="checkbox"
                checked={draft.confirmOnClose}
                onChange={(e) => update("confirmOnClose", e.target.checked)}
              />
              <span>Confirm before closing the application with open sessions</span>
            </label>
            <label style={row}>
              <input
                aria-label="Auto reconnect"
                type="checkbox"
                checked={draft.autoReconnect}
                onChange={(e) => update("autoReconnect", e.target.checked)}
              />
              <span>Automatically reconnect dropped sessions</span>
            </label>
            <label style={row}>
              <span style={lbl}>Max reconnect attempts</span>
              <input
                aria-label="Reconnect max attempts"
                type="number"
                min={0}
                max={50}
                disabled={!draft.autoReconnect}
                value={draft.reconnectMaxN}
                onChange={(e) =>
                  update("reconnectMaxN", Number(e.target.value) || 0)
                }
              />
            </label>
            <label style={row}>
              <span style={lbl}>Reconnect delay (ms)</span>
              <input
                aria-label="Reconnect delay ms"
                type="number"
                min={0}
                max={60000}
                step={100}
                disabled={!draft.autoReconnect}
                value={draft.reconnectDelayMs}
                onChange={(e) =>
                  update("reconnectDelayMs", Number(e.target.value) || 0)
                }
              />
            </label>
          </fieldset>

          <fieldset style={fieldset}>
            <legend>Privacy</legend>
            <label style={row}>
              <input
                aria-label="Telemetry"
                type="checkbox"
                checked={draft.telemetryEnabled}
                onChange={(e) => update("telemetryEnabled", e.target.checked)}
              />
              <span>Share anonymous usage telemetry</span>
            </label>
            <p style={{ color: "var(--fg-muted)", fontSize: 12, margin: 0 }}>
              Off by default. Nothing is sent without your consent.
            </p>
          </fieldset>

          <footer
            style={{
              display: "flex",
              gap: 8,
              alignItems: "center",
              borderTop: "1px solid var(--border)",
              paddingTop: 12,
            }}
          >
            <span
              style={{ color: "var(--fg-muted)", fontSize: 12 }}
              data-testid="settings-dirty-indicator"
            >
              {dirty ? "Unsaved changes" : "Saved"}
            </span>
            <div style={{ flex: 1 }} />
            <button
              onClick={onRevert}
              disabled={!dirty || saving}
              aria-label="Revert"
            >
              Revert
            </button>
            <button
              onClick={onSave}
              disabled={!dirty || saving || clientErrors.length > 0}
              aria-label="Save settings"
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </footer>
        </>
      )}
    </section>
  );
}

function shallowSettingsEqual(a: Settings, b: Settings): boolean {
  return (
    a.theme === b.theme &&
    a.fontFamily === b.fontFamily &&
    a.fontSizePx === b.fontSizePx &&
    a.confirmOnClose === b.confirmOnClose &&
    a.autoReconnect === b.autoReconnect &&
    a.reconnectMaxN === b.reconnectMaxN &&
    a.reconnectDelayMs === b.reconnectDelayMs &&
    a.telemetryEnabled === b.telemetryEnabled
  );
}

const fieldset: React.CSSProperties = {
  border: "1px solid var(--border)",
  borderRadius: 4,
  padding: 12,
  display: "flex",
  flexDirection: "column",
  gap: 8,
};

const row: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 8,
};

const lbl: React.CSSProperties = {
  minWidth: 180,
  color: "var(--fg-muted)",
  fontSize: 13,
};
