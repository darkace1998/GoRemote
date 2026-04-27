import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  type Dispatch,
  type ReactNode,
} from "react";
import { bridge } from "../bridge";
import {
  workspaceAPI,
  type Workspace,
  type TabState as PersistedTabState,
} from "../api/workspace";
import { debounce } from "../util/debounce";
import type {
  Notification,
  Protocol,
  Tab,
  TreeNode,
  WarningSeverity,
} from "../types";

export interface AppState {
  tree: TreeNode[];
  treeLoading: boolean;
  selectedNodeId: string | null;
  searchQuery: string;
  tabs: Tab[];
  activeTabId: string | null;
  /**
   * True once the persisted workspace has been loaded (or the load
   * failed and we fell back to defaults). Save side effects are gated
   * on this flag so we don't overwrite the user's saved state with the
   * empty initial state during boot.
   */
  workspaceHydrated: boolean;
  notifications: Notification[];
  quickConnectOpen: boolean;
  importDialogOpen: boolean;
}

/** Action union. Keep each case documented so reducers stay self-describing. */
export type Action =
  /** Replace connection tree (e.g. after listConnections or import). */
  | { type: "tree/set"; tree: TreeNode[] }
  /** Toggle folder collapse state by id. */
  | { type: "tree/toggleCollapse"; id: string }
  /** Set the currently selected tree node. */
  | { type: "tree/select"; id: string | null }
  /** Update the sidebar search filter. */
  | { type: "search/set"; query: string }
  /** Set loading indicator while tree is fetched. */
  | { type: "tree/loading"; loading: boolean }
  /** Append a new tab and activate it. */
  | { type: "tabs/open"; tab: Tab }
  /** Close a tab by id. */
  | { type: "tabs/close"; id: string }
  /** Activate an existing tab. */
  | { type: "tabs/activate"; id: string }
  /** Cycle to next/prev tab (Ctrl+Tab / Ctrl+Shift+Tab). */
  | { type: "tabs/cycle"; direction: 1 | -1 }
  /**
   * Replace tabs/activeTab from a persisted workspace document. This
   * action is dispatched once on boot after GetWorkspace resolves.
   */
  | { type: "workspace/hydrate"; tabs: Tab[]; activeTabId: string | null }
  /** Reorder open tabs (drag-and-drop). */
  | { type: "tabs/reorder"; order: string[] }
  /** Push a transient notification onto the toast stack. */
  | { type: "notify/push"; notification: Notification }
  /** Dismiss notification by id. */
  | { type: "notify/dismiss"; id: string }
  /** Show/hide the quick-connect modal. */
  | { type: "quickConnect/setOpen"; open: boolean }
  /** Show/hide the import dialog. */
  | { type: "import/setOpen"; open: boolean }
  /** Rename a tree node by id (local UI rename — backend persistence is a TODO). */
  | { type: "tree/rename"; id: string; name: string };

const initialState: AppState = {
  tree: [],
  treeLoading: false,
  selectedNodeId: null,
  searchQuery: "",
  tabs: [],
  activeTabId: null,
  workspaceHydrated: false,
  notifications: [],
  quickConnectOpen: false,
  importDialogOpen: false,
};

function toggleCollapse(nodes: TreeNode[], id: string): TreeNode[] {
  return nodes.map((n) => {
    if (n.kind === "folder") {
      if (n.id === id) return { ...n, collapsed: !n.collapsed };
      return { ...n, children: toggleCollapse(n.children, id) };
    }
    return n;
  });
}

function renameNode(nodes: TreeNode[], id: string, name: string): TreeNode[] {
  return nodes.map((n) => {
    if (n.id === id) return { ...n, name };
    if (n.kind === "folder") {
      return { ...n, children: renameNode(n.children, id, name) };
    }
    return n;
  });
}

function reducer(state: AppState, action: Action): AppState {
  switch (action.type) {
    case "tree/set":
      return { ...state, tree: action.tree, treeLoading: false };
    case "tree/loading":
      return { ...state, treeLoading: action.loading };
    case "tree/toggleCollapse":
      return { ...state, tree: toggleCollapse(state.tree, action.id) };
    case "tree/select":
      return { ...state, selectedNodeId: action.id };
    case "search/set":
      return { ...state, searchQuery: action.query };
    case "tabs/open": {
      const exists = state.tabs.find((t) => t.id === action.tab.id);
      const tabs = exists ? state.tabs : [...state.tabs, action.tab];
      return { ...state, tabs, activeTabId: action.tab.id };
    }
    case "tabs/close": {
      const tabs = state.tabs.filter((t) => t.id !== action.id);
      let activeTabId = state.activeTabId;
      if (activeTabId === action.id) {
        activeTabId = tabs.length > 0 ? tabs[tabs.length - 1].id : null;
      }
      return { ...state, tabs, activeTabId };
    }
    case "tabs/activate":
      return { ...state, activeTabId: action.id };
    case "tabs/cycle": {
      if (state.tabs.length === 0) return state;
      const idx = state.tabs.findIndex((t) => t.id === state.activeTabId);
      const next =
        (idx + action.direction + state.tabs.length) % state.tabs.length;
      return { ...state, activeTabId: state.tabs[next].id };
    }
    case "tabs/reorder": {
      const byId = new Map(state.tabs.map((t) => [t.id, t]));
      const reordered: Tab[] = [];
      for (const id of action.order) {
        const t = byId.get(id);
        if (t) {
          reordered.push(t);
          byId.delete(id);
        }
      }
      // Append any tabs not present in the reorder list to avoid losing
      // state if the caller passed an incomplete order.
      for (const t of byId.values()) reordered.push(t);
      return { ...state, tabs: reordered };
    }
    case "workspace/hydrate": {
      // If the persisted document is empty but tabs are already present
      // in memory (e.g., a deep-link or test that seeded tabs before the
      // async load resolved), keep them and just flip the hydrated flag.
      if (action.tabs.length === 0 && state.tabs.length > 0) {
        return { ...state, workspaceHydrated: true };
      }
      return {
        ...state,
        tabs: action.tabs,
        activeTabId: action.activeTabId,
        workspaceHydrated: true,
      };
    }
    case "notify/push":
      return {
        ...state,
        notifications: [...state.notifications, action.notification],
      };
    case "notify/dismiss":
      return {
        ...state,
        notifications: state.notifications.filter((n) => n.id !== action.id),
      };
    case "quickConnect/setOpen":
      return { ...state, quickConnectOpen: action.open };
    case "import/setOpen":
      return { ...state, importDialogOpen: action.open };
    case "tree/rename":
      return { ...state, tree: renameNode(state.tree, action.id, action.name) };
    default:
      return state;
  }
}

const StateCtx = createContext<AppState | null>(null);
const DispatchCtx = createContext<Dispatch<Action> | null>(null);

export const WORKSPACE_SAVE_DEBOUNCE_MS = 500;

/** Map a persisted TabState onto the in-memory Tab type. */
function persistedToTab(p: PersistedTabState, tree: TreeNode[]): Tab {
  return {
    id: p.id,
    title: p.title,
    sessionHandle: "",
    connectionId: p.connectionId,
    protocol: lookupProtocol(tree, p.connectionId) ?? "ssh",
  };
}

function lookupProtocol(tree: TreeNode[], connectionId: string): Protocol | undefined {
  for (const node of tree) {
    if (node.kind === "connection" && node.id === connectionId) {
      return node.protocol;
    }
    if (node.kind === "folder") {
      const found = lookupProtocol(node.children, connectionId);
      if (found) return found;
    }
  }
  return undefined;
}

/** Map the in-memory tabs onto the persisted Workspace document. */
export function tabsToWorkspace(
  tabs: Tab[],
  activeTabId: string | null,
): Workspace {
  const now = new Date().toISOString();
  return {
    version: 1,
    openTabs: tabs.map((t) => ({
      id: t.id,
      connectionId: t.connectionId ?? "",
      title: t.title,
      lastUsedAt: now,
    })),
    activeTab:
      activeTabId && tabs.some((t) => t.id === activeTabId)
        ? activeTabId
        : undefined,
    updatedAt: now,
  };
}

export function AppStateProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);

  // Hydrate the connection tree (existing behaviour).
  useEffect(() => {
    let cancelled = false;
    dispatch({ type: "tree/loading", loading: true });
    bridge
      .listConnections()
      .then((tree) => {
        if (!cancelled) dispatch({ type: "tree/set", tree });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        dispatch({
          type: "notify/push",
          notification: {
            id: `err-${Date.now()}`,
            severity: "error",
            message: `Failed to load connections: ${String(err)}`,
          },
        });
        dispatch({ type: "tree/loading", loading: false });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Track the latest tree without re-running the hydration effect: when
  // the tree resolves after the workspace document, we still want
  // restored tabs to pick up the correct protocol.
  const treeRef = useRef<TreeNode[]>(state.tree);
  useEffect(() => {
    treeRef.current = state.tree;
  }, [state.tree]);

  // Hydrate the persisted workspace once on mount.
  useEffect(() => {
    let cancelled = false;
    workspaceAPI()
      .get()
      .then((w) => {
        if (cancelled) return;
        const tabs = w.openTabs.map((p) => persistedToTab(p, treeRef.current));
        const activeTabId =
          w.activeTab && tabs.some((t) => t.id === w.activeTab)
            ? w.activeTab
            : tabs.length > 0
              ? tabs[tabs.length - 1].id
              : null;
        dispatch({ type: "workspace/hydrate", tabs, activeTabId });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        // Even on error we mark hydration complete so save side effects
        // can begin; otherwise a transient backend issue would block all
        // future saves for the session.
        dispatch({ type: "workspace/hydrate", tabs: [], activeTabId: null });
        dispatch({
          type: "notify/push",
          notification: {
            id: `wsh-${Date.now()}`,
            severity: "warning",
            message: `Failed to restore workspace: ${String(err)}`,
          },
        });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Debounced save: collapse rapid tab/active changes into a single
  // SaveWorkspace call after the configured wait window. Errors are
  // surfaced as toasts but do not throw out of the effect.
  const debouncedSaveRef = useRef<ReturnType<
    typeof debounce<[Workspace]>
  > | null>(null);
  if (debouncedSaveRef.current === null) {
    debouncedSaveRef.current = debounce(async (w: Workspace) => {
      try {
        await workspaceAPI().save(w);
      } catch (err) {
        dispatch({
          type: "notify/push",
          notification: {
            id: `wss-${Date.now()}`,
            severity: "warning",
            message: `Failed to save workspace: ${String(err)}`,
          },
        });
      }
    }, WORKSPACE_SAVE_DEBOUNCE_MS);
  }

  useEffect(() => {
    if (!state.workspaceHydrated) return;
    const w = tabsToWorkspace(state.tabs, state.activeTabId);
    debouncedSaveRef.current?.(w);
  }, [state.workspaceHydrated, state.tabs, state.activeTabId]);

  // Cancel any pending save on unmount so tests don't leak timers.
  useEffect(() => {
    return () => {
      debouncedSaveRef.current?.cancel();
    };
  }, []);

  const stateMemo = useMemo(() => state, [state]);

  return (
    <StateCtx.Provider value={stateMemo}>
      <DispatchCtx.Provider value={dispatch}>{children}</DispatchCtx.Provider>
    </StateCtx.Provider>
  );
}

export function useAppState(): AppState {
  const v = useContext(StateCtx);
  if (!v) throw new Error("useAppState must be used within AppStateProvider");
  return v;
}

export function useAppDispatch(): Dispatch<Action> {
  const v = useContext(DispatchCtx);
  if (!v)
    throw new Error("useAppDispatch must be used within AppStateProvider");
  return v;
}

export function notify(
  dispatch: Dispatch<Action>,
  severity: WarningSeverity,
  message: string,
  timeoutMs = 5000,
): void {
  const id = `n-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
  dispatch({
    type: "notify/push",
    notification: { id, severity, message, timeoutMs },
  });
}
