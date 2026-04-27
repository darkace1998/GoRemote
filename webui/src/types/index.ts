export type Protocol = "ssh" | "telnet" | "raw" | "rlogin";

export const PROTOCOLS: Protocol[] = ["ssh", "telnet", "raw", "rlogin"];

export interface ConnectionNode {
  kind: "connection";
  id: string;
  name: string;
  protocol: Protocol;
  host: string;
  port: number;
  username?: string;
  tags?: string[];
  credentialRef?: string;
}

export interface FolderNode {
  kind: "folder";
  id: string;
  name: string;
  tags?: string[];
  children: TreeNode[];
  collapsed?: boolean;
}

export type TreeNode = FolderNode | ConnectionNode;

export type SessionHandle = string;

export interface QuickConnectRequest {
  protocol: Protocol;
  host: string;
  port: number;
  username?: string;
}

export type WarningSeverity = "info" | "warning" | "error";

export interface Warning {
  severity: WarningSeverity;
  code: string;
  message: string;
  path?: string;
  field?: string;
}

export interface ImportResult {
  importedCount: number;
  warnings: Warning[];
  rootNodeIds: string[];
}

export interface Tab {
  id: string;
  title: string;
  sessionHandle: SessionHandle;
  connectionId?: string;
  protocol: Protocol;
}

export interface Notification {
  id: string;
  severity: WarningSeverity;
  message: string;
  timeoutMs?: number;
}
