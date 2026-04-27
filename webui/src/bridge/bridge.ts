import type {
  ImportResult,
  QuickConnectRequest,
  SessionHandle,
  TreeNode,
} from "../types";

export type OutputHandler = (chunk: string) => void;
export type Unsubscribe = () => void;

/**
 * Bridge is the single boundary between the UI and the Go app core.
 * All mutations and protocol/session interactions route through here.
 * Implementations: wailsBridge (production), mockBridge (dev + tests).
 */
export interface Bridge {
  listConnections(): Promise<TreeNode[]>;
  quickConnect(req: QuickConnectRequest): Promise<SessionHandle>;
  openSession(connectionId: string): Promise<SessionHandle>;
  closeSession(handle: SessionHandle): Promise<void>;
  sendInput(handle: SessionHandle, data: string): Promise<void>;
  resize(handle: SessionHandle, cols: number, rows: number): Promise<void>;
  subscribeOutput(handle: SessionHandle, onData: OutputHandler): Unsubscribe;
  importMRemoteNG(fileName: string, contents: string): Promise<ImportResult>;
}
