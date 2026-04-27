import type { Bridge, OutputHandler, Unsubscribe } from "./bridge";
import type {
  ImportResult,
  QuickConnectRequest,
  SessionHandle,
  TreeNode,
} from "../types";

const demoTree: TreeNode[] = [
  {
    kind: "folder",
    id: "folder-lab",
    name: "Lab",
    children: [
      {
        kind: "connection",
        id: "conn-lab-router",
        name: "Lab Router",
        protocol: "ssh",
        host: "10.0.0.1",
        port: 22,
        username: "admin",
        tags: ["network", "lab"],
      },
      {
        kind: "connection",
        id: "conn-lab-switch",
        name: "Lab Switch",
        protocol: "telnet",
        host: "10.0.0.2",
        port: 23,
        tags: ["network", "lab"],
      },
    ],
  },
];

interface SessionState {
  handlers: Set<OutputHandler>;
  timer: ReturnType<typeof setInterval>;
  tickCount: number;
}

const sessions = new Map<SessionHandle, SessionState>();

function openLocalSession(label: string): SessionHandle {
  const handle = `mock-${label}-${Math.random().toString(36).slice(2, 8)}`;
  const state: SessionState = {
    handlers: new Set(),
    tickCount: 0,
    timer: setInterval(() => {
      state.tickCount += 1;
      const msg = `\r\n[mock ${label}] tick ${state.tickCount}\r\n`;
      for (const h of state.handlers) h(msg);
    }, 5000),
  };
  sessions.set(handle, state);
  // Initial banner after a tick so subscribers attach first.
  setTimeout(() => {
    const s = sessions.get(handle);
    if (!s) return;
    const banner = `\x1b[1;32mgoremote mock session\x1b[0m (${label})\r\nType to echo. This is a development-only bridge.\r\n$ `;
    for (const h of s.handlers) h(banner);
  }, 50);
  return handle;
}

export const mockBridge: Bridge = {
  async listConnections() {
    return structuredClone(demoTree);
  },
  async quickConnect(req: QuickConnectRequest) {
    return openLocalSession(`${req.protocol}:${req.host}`);
  },
  async openSession(connectionId: string) {
    return openLocalSession(connectionId);
  },
  async closeSession(handle: SessionHandle) {
    const s = sessions.get(handle);
    if (!s) return;
    clearInterval(s.timer);
    s.handlers.clear();
    sessions.delete(handle);
  },
  async sendInput(handle: SessionHandle, data: string) {
    const s = sessions.get(handle);
    if (!s) return;
    const echoed = data === "\r" ? "\r\n$ " : data;
    for (const h of s.handlers) h(echoed);
  },
  async resize(_handle: SessionHandle, _cols: number, _rows: number) {
    // mock: no-op
  },
  subscribeOutput(handle: SessionHandle, onData: OutputHandler): Unsubscribe {
    const s = sessions.get(handle);
    if (!s) return () => undefined;
    s.handlers.add(onData);
    return () => {
      s.handlers.delete(onData);
    };
  },
  async importMRemoteNG(fileName: string, _contents: string): Promise<ImportResult> {
    return {
      importedCount: 0,
      rootNodeIds: [],
      warnings: [
        {
          severity: "info",
          code: "MOCK_IMPORT",
          message: `Mock bridge does not parse ${fileName}; wire wailsBridge for real import.`,
        },
      ],
    };
  },
};
