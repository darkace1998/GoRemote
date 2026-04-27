import type { Bridge, OutputHandler, Unsubscribe } from "./bridge";
import type {
  ImportResult,
  QuickConnectRequest,
  SessionHandle,
  TreeNode,
} from "../types";

interface WailsApp {
  ListConnections?: () => Promise<TreeNode[]>;
  QuickConnect?: (req: QuickConnectRequest) => Promise<SessionHandle>;
  OpenSession?: (connectionId: string) => Promise<SessionHandle>;
  CloseSession?: (handle: SessionHandle) => Promise<void>;
  SendInput?: (handle: SessionHandle, data: string) => Promise<void>;
  Resize?: (handle: SessionHandle, cols: number, rows: number) => Promise<void>;
  ImportMRemoteNG?: (
    fileName: string,
    contents: string,
  ) => Promise<ImportResult>;
}

interface WailsRuntime {
  EventsOn?: (name: string, cb: (...args: unknown[]) => void) => () => void;
  EventsOff?: (name: string) => void;
}

function app(): WailsApp | undefined {
  return (window as unknown as { go?: { main?: { App?: WailsApp } } }).go?.main
    ?.App;
}

function runtime(): WailsRuntime | undefined {
  return (window as unknown as { runtime?: WailsRuntime }).runtime;
}

function warnMissing(method: string): void {
  // eslint-disable-next-line no-console
  console.warn(`[wailsBridge] ${method} unavailable; wails runtime not bound`);
}

export const wailsBridge: Bridge = {
  async listConnections() {
    const a = app();
    if (!a?.ListConnections) {
      warnMissing("ListConnections");
      return [];
    }
    return a.ListConnections();
  },
  async quickConnect(req) {
    const a = app();
    if (!a?.QuickConnect) {
      warnMissing("QuickConnect");
      throw new Error("QuickConnect unavailable");
    }
    return a.QuickConnect(req);
  },
  async openSession(connectionId) {
    const a = app();
    if (!a?.OpenSession) {
      warnMissing("OpenSession");
      throw new Error("OpenSession unavailable");
    }
    return a.OpenSession(connectionId);
  },
  async closeSession(handle) {
    const a = app();
    if (!a?.CloseSession) {
      warnMissing("CloseSession");
      return;
    }
    return a.CloseSession(handle);
  },
  async sendInput(handle, data) {
    const a = app();
    if (!a?.SendInput) {
      warnMissing("SendInput");
      return;
    }
    return a.SendInput(handle, data);
  },
  async resize(handle, cols, rows) {
    const a = app();
    if (!a?.Resize) {
      warnMissing("Resize");
      return;
    }
    return a.Resize(handle, cols, rows);
  },
  subscribeOutput(handle: SessionHandle, onData: OutputHandler): Unsubscribe {
    const rt = runtime();
    if (!rt?.EventsOn) {
      warnMissing("EventsOn");
      return () => undefined;
    }
    const eventName = `session.output.${handle}`;
    const off = rt.EventsOn(eventName, (...args: unknown[]) => {
      const chunk = args[0];
      if (typeof chunk === "string") {
        onData(chunk);
      }
    });
    return () => off();
  },
  async importMRemoteNG(fileName, contents) {
    const a = app();
    if (!a?.ImportMRemoteNG) {
      warnMissing("ImportMRemoteNG");
      throw new Error("ImportMRemoteNG unavailable");
    }
    return a.ImportMRemoteNG(fileName, contents);
  },
};
