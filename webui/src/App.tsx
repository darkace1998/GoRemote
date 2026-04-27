import { useEffect, useState } from "react";
import { SearchBox } from "./components/Sidebar/SearchBox";
import { Tree } from "./components/Sidebar/Tree";
import { Tabs } from "./components/Workspace/Tabs";
import { TerminalPane } from "./components/Workspace/TerminalPane";
import { QuickConnectModal } from "./components/QuickConnect/Modal";
import { ImportDialog } from "./components/ImportDialog";
import { ToastContainer } from "./components/Notifications/ToastContainer";
import { useAppDispatch, useAppState } from "./state/store";
import { useTheme } from "./theme/useTheme";
import { bridge } from "./bridge";
import { useKeymapBindings } from "./keymap/useKeymap";
import { ShortcutHelpModal } from "./keymap/ShortcutHelpModal";
import { LiveAnnouncer, useAnnounce } from "./a11y/announce";

function AppInner() {
  const { tabs, activeTabId, quickConnectOpen, importDialogOpen } =
    useAppState();
  const dispatch = useAppDispatch();
  const { mode, setMode, toggle } = useTheme();
  const announce = useAnnounce();
  const [helpOpen, setHelpOpen] = useState(false);
  const [renameRequest, setRenameRequest] = useState(0);

  // Reflect tab activation in the live region for screen-reader users.
  useEffect(() => {
    if (!activeTabId) return;
    const t = tabs.find((x) => x.id === activeTabId);
    if (t) announce(`Active tab: ${t.title}`);
  }, [activeTabId, tabs, announce]);

  useKeymapBindings(
    [
      {
        id: "new-connection",
        keys: ["Mod+t"],
        description: "Focus connection tree / open Quick Connect",
        group: "Connection",
        handler: () => {
          // Focus the tree first, then open quick connect (acts as the
          // quick-launch palette in this build).
          const tree = document.querySelector<HTMLElement>('[role="tree"]');
          tree?.focus();
          dispatch({ type: "quickConnect/setOpen", open: true });
          announce("Quick connect opened");
        },
      },
      {
        id: "close-tab",
        keys: ["Mod+w"],
        description: "Close active tab",
        group: "Tabs",
        handler: () => {
          if (!activeTabId) return;
          // ConfirmOnClose lives in the user settings; we keep this binding
          // simple and always close. The Settings UI surfaces the preference
          // for application-quit confirmation.
          void bridge.closeSession(activeTabId);
          dispatch({ type: "tabs/close", id: activeTabId });
          announce("Tab closed");
        },
      },
      {
        id: "next-tab",
        keys: ["Ctrl+Tab"],
        description: "Next tab",
        group: "Tabs",
        handler: () => dispatch({ type: "tabs/cycle", direction: 1 }),
      },
      {
        id: "prev-tab",
        keys: ["Ctrl+Shift+Tab"],
        description: "Previous tab",
        group: "Tabs",
        handler: () => dispatch({ type: "tabs/cycle", direction: -1 }),
      },
      ...[1, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => ({
        id: `tab-${n}`,
        keys: [`Ctrl+${n}`],
        description: `Switch to tab ${n}`,
        group: "Tabs",
        handler: () => {
          const t = tabs[n - 1];
          if (t) dispatch({ type: "tabs/activate", id: t.id });
        },
      })),
      {
        id: "rename-node",
        keys: ["F2"],
        description: "Rename selected tree node",
        group: "Connection",
        handler: () => setRenameRequest((x) => x + 1),
      },
      {
        id: "show-help",
        keys: ["?", "Shift+?"],
        description: "Show keyboard shortcuts",
        group: "Help",
        handler: () => setHelpOpen(true),
      },
    ],
    [dispatch, activeTabId, tabs, announce],
  );

  return (
    <div className="app-shell">
      <header className="toolbar" role="toolbar" aria-label="Primary">
        <strong>goremote</strong>
        <button
          onClick={() => dispatch({ type: "quickConnect/setOpen", open: true })}
          aria-label="Quick connect"
        >
          Quick Connect
        </button>
        <button
          onClick={() => dispatch({ type: "import/setOpen", open: true })}
          aria-label="Import mRemoteNG file"
        >
          Import…
        </button>
        <button
          onClick={() => setHelpOpen(true)}
          aria-label="Show keyboard shortcuts"
          title="Keyboard shortcuts (?)"
        >
          ?
        </button>
        <div className="spacer" />
        <label
          htmlFor="theme-select"
          style={{ fontSize: 12, color: "var(--fg-muted)" }}
        >
          Theme
        </label>
        <select
          id="theme-select"
          value={mode}
          onChange={(e) =>
            setMode(e.target.value as "light" | "dark" | "system")
          }
        >
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
        <button onClick={toggle} aria-label="Toggle light/dark theme">
          Toggle
        </button>
      </header>

      <div className="workspace">
        <aside className="sidebar" aria-label="Connections">
          <SearchBox />
          <Tree renameRequest={renameRequest} />
        </aside>

        <main className="workspace-main">
          <Tabs />
          <div
            className="tab-panels"
            style={{ minHeight: 0, position: "relative" }}
          >
            {tabs.length === 0 && (
              <div
                style={{
                  padding: 24,
                  color: "var(--fg-muted)",
                  height: "100%",
                }}
              >
                No active sessions. Double-click a connection in the sidebar or
                use <b>Quick Connect</b>.
              </div>
            )}
            {tabs.map((t) => (
              <div
                key={t.id}
                role="tabpanel"
                id={`tabpanel-${t.id}`}
                aria-labelledby={`tab-${t.id}`}
                hidden={t.id !== activeTabId}
                style={{
                  position: "absolute",
                  inset: 0,
                  display: t.id === activeTabId ? "block" : "none",
                }}
              >
                <TerminalPane
                  handle={t.sessionHandle}
                  visible={t.id === activeTabId}
                />
              </div>
            ))}
          </div>
        </main>
      </div>

      <footer className="statusbar" role="status" aria-live="off">
        <span>
          Sessions: {tabs.length}
          {activeTabId ? ` · Active: ${activeTabId}` : ""}
        </span>
        <span style={{ marginLeft: "auto" }}>Theme: {mode}</span>
      </footer>

      {quickConnectOpen && (
        <QuickConnectModal
          onClose={() => dispatch({ type: "quickConnect/setOpen", open: false })}
        />
      )}
      {importDialogOpen && (
        <ImportDialog
          onClose={() => dispatch({ type: "import/setOpen", open: false })}
        />
      )}
      {helpOpen && <ShortcutHelpModal onClose={() => setHelpOpen(false)} />}
      <ToastContainer />
    </div>
  );
}

export default function App() {
  return (
    <LiveAnnouncer>
      <AppInner />
    </LiveAnnouncer>
  );
}
