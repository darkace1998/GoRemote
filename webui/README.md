# goremote web UI

Vite + React + TypeScript shell for goremote. Hosted inside the Wails desktop
binary (`cmd/desktop`) in production; runs standalone against the in-memory
mock bridge during development.

## Requirements

- Node.js ≥ 18 (Debian trixie ships 20; any 18/20/22 LTS works).
- `npm install` once to hydrate `node_modules/`.

## Scripts

```
npm run dev        # Vite dev server on :5173 using mockBridge
npm run build      # tsc -b && vite build → dist/ (consumed by Wails)
npm run preview    # Serve the built bundle locally
npm run typecheck  # tsc --noEmit (no project references emit)
```

## Bridge auto-selection

`src/bridge/index.ts` picks an implementation at runtime:

- `wailsBridge` — used when `window.go.main.App` is bound (Wails desktop).
  All Go methods are called as `App.Method(...)`; async output is delivered
  via the Wails `runtime.EventsOn("session.output.<handle>", ...)` channel.
- `mockBridge` — used in the browser during `npm run dev`. Ships two demo
  connections, echoes input back to the terminal, and emits a periodic tick
  so the terminal pane has something to render.

The selection is done with:

```ts
export const bridge = ((window as any).go?.main?.App) ? wailsBridge : mockBridge;
```

## Adding a new bridge method

1. Extend the `Bridge` interface in `src/bridge/bridge.ts` with a strongly
   typed method signature. Keep arguments serializable — they cross the
   Wails IPC boundary.
2. Implement the method in `src/bridge/mockBridge.ts` first (so the dev
   server keeps working without Go).
3. Implement the method in `src/bridge/wailsBridge.ts`, guarding against
   a missing runtime with `warnMissing(...)`.
4. Expose the corresponding Go method on the Wails `App` struct in
   `cmd/desktop` (PascalCase; it becomes `window.go.main.App.Xxx`).
5. Route UI mutations through the dispatch/reducer in `src/state/store.tsx`;
   do not let components mutate shared state directly.

## Architectural notes

- The UI is declarative and state-driven per `architecture.md` §4: the
  reducer owns all visible state, commands route through the bridge, and
  protocol-specific concerns live outside the UI.
- Terminal rendering uses `@xterm/xterm` + `@xterm/addon-fit`. A
  `ResizeObserver` on the host element recalls `fit()` and pushes the new
  dimensions via `bridge.resize(...)`.
- Secrets must not be persisted in the tree. Connection nodes carry a
  `credentialRef`; resolution happens in the Go core.
- Themes are CSS-variable based (`src/theme/theme.css`) with a
  `prefers-color-scheme` hook (`useTheme`) that persists choice in
  `localStorage`.
- Accessibility: focus-visible rings, `aria-*` on tree/tabs/toasts,
  keyboard shortcuts (`Ctrl+N` quick connect, `Ctrl+W` close tab,
  `Ctrl+Tab` cycle tabs).
