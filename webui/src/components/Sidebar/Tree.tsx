import classNames from "classnames";
import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { bridge } from "../../bridge";
import { notify, useAppDispatch, useAppState } from "../../state/store";
import type { TreeNode } from "../../types";

function matches(node: TreeNode, q: string): boolean {
  if (!q) return true;
  const needle = q.toLowerCase();
  if (node.name.toLowerCase().includes(needle)) return true;
  if (node.tags?.some((t) => t.toLowerCase().includes(needle))) return true;
  if (node.kind === "folder") {
    return node.children.some((c) => matches(c, q));
  }
  return false;
}

interface FlatItem {
  node: TreeNode;
  depth: number;
  parentId: string | null;
  visible: boolean; // hidden if an ancestor is collapsed
}

/** Flattens the tree in DOM order, marking which items are visible. */
function flatten(
  nodes: TreeNode[],
  query: string,
  out: FlatItem[] = [],
  depth = 0,
  parentId: string | null = null,
  visible = true,
): FlatItem[] {
  for (const n of nodes) {
    if (!matches(n, query)) continue;
    out.push({ node: n, depth, parentId, visible });
    if (n.kind === "folder") {
      const childrenVisible = visible && !(n.collapsed ?? false);
      flatten(n.children, query, out, depth + 1, n.id, childrenVisible);
    }
  }
  return out;
}

interface Props {
  /** Increments to request rename of the currently selected node. */
  renameRequest?: number;
}

export function Tree({ renameRequest = 0 }: Props) {
  const { tree, searchQuery, treeLoading, selectedNodeId } = useAppState();
  const dispatch = useAppDispatch();

  const flat = useMemo(() => flatten(tree, searchQuery), [tree, searchQuery]);
  const visibleItems = flat.filter((f) => f.visible);

  // Roving tabindex / focus tracking.
  const [focusedId, setFocusedId] = useState<string | null>(null);
  useEffect(() => {
    if (visibleItems.length === 0) {
      setFocusedId(null);
      return;
    }
    if (!focusedId || !visibleItems.some((i) => i.node.id === focusedId)) {
      setFocusedId(selectedNodeId ?? visibleItems[0].node.id);
    }
  }, [visibleItems, focusedId, selectedNodeId]);

  const itemRefs = useRef<Record<string, HTMLDivElement | null>>({});

  const focusItem = (id: string) => {
    setFocusedId(id);
    // Defer to next tick: tabindex flip needs to render first.
    queueMicrotask(() => itemRefs.current[id]?.focus());
  };

  // Inline rename state.
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingValue, setEditingValue] = useState("");
  const lastRename = useRef(0);
  useEffect(() => {
    if (renameRequest === lastRename.current) return;
    lastRename.current = renameRequest;
    if (renameRequest === 0) return;
    const target = selectedNodeId ?? focusedId;
    if (!target) return;
    const item = flat.find((i) => i.node.id === target);
    if (!item) return;
    setEditingId(target);
    setEditingValue(item.node.name);
  }, [renameRequest, selectedNodeId, focusedId, flat]);

  const commitRename = () => {
    if (editingId && editingValue.trim()) {
      dispatch({
        type: "tree/rename",
        id: editingId,
        name: editingValue.trim(),
      });
    }
    setEditingId(null);
    setEditingValue("");
  };

  const openConnection = async (id: string) => {
    const node = flat.find((i) => i.node.id === id)?.node;
    if (!node || node.kind !== "connection") return;
    try {
      const handle = await bridge.openSession(node.id);
      dispatch({
        type: "tabs/open",
        tab: {
          id: handle,
          title: node.name,
          sessionHandle: handle,
          connectionId: node.id,
          protocol: node.protocol,
        },
      });
    } catch (err) {
      notify(dispatch, "error", `Failed to open session: ${String(err)}`);
    }
  };

  const onKeyDown = (
    e: ReactKeyboardEvent<HTMLDivElement>,
    item: FlatItem,
  ) => {
    const idx = visibleItems.findIndex((i) => i.node.id === item.node.id);
    if (idx < 0) return;
    const node = item.node;
    switch (e.key) {
      case "ArrowDown": {
        e.preventDefault();
        const next = visibleItems[idx + 1];
        if (next) {
          focusItem(next.node.id);
          dispatch({ type: "tree/select", id: next.node.id });
        }
        return;
      }
      case "ArrowUp": {
        e.preventDefault();
        const prev = visibleItems[idx - 1];
        if (prev) {
          focusItem(prev.node.id);
          dispatch({ type: "tree/select", id: prev.node.id });
        }
        return;
      }
      case "ArrowRight": {
        e.preventDefault();
        if (node.kind === "folder") {
          if (node.collapsed) {
            dispatch({ type: "tree/toggleCollapse", id: node.id });
          } else {
            // Move to first child if any are visible.
            const firstChild = visibleItems[idx + 1];
            if (firstChild && firstChild.parentId === node.id) {
              focusItem(firstChild.node.id);
              dispatch({ type: "tree/select", id: firstChild.node.id });
            }
          }
        }
        return;
      }
      case "ArrowLeft": {
        e.preventDefault();
        if (node.kind === "folder" && !(node.collapsed ?? false)) {
          dispatch({ type: "tree/toggleCollapse", id: node.id });
        } else if (item.parentId) {
          focusItem(item.parentId);
          dispatch({ type: "tree/select", id: item.parentId });
        }
        return;
      }
      case "Enter": {
        e.preventDefault();
        if (node.kind === "folder") {
          dispatch({ type: "tree/toggleCollapse", id: node.id });
        } else {
          void openConnection(node.id);
        }
        return;
      }
      case " ": {
        e.preventDefault();
        dispatch({ type: "tree/select", id: node.id });
        return;
      }
      case "Home": {
        e.preventDefault();
        const first = visibleItems[0];
        if (first) {
          focusItem(first.node.id);
          dispatch({ type: "tree/select", id: first.node.id });
        }
        return;
      }
      case "End": {
        e.preventDefault();
        const last = visibleItems[visibleItems.length - 1];
        if (last) {
          focusItem(last.node.id);
          dispatch({ type: "tree/select", id: last.node.id });
        }
        return;
      }
      default:
        return;
    }
  };

  if (treeLoading && tree.length === 0) {
    return (
      <div className="tree" style={{ padding: 8, color: "var(--fg-muted)" }}>
        Loading…
      </div>
    );
  }
  if (visibleItems.length === 0) {
    return (
      <div className="tree" style={{ padding: 8, color: "var(--fg-muted)" }}>
        {tree.length === 0 ? (
          <>
            No connections. Use <b>Quick Connect</b> or <b>Import</b>.
          </>
        ) : (
          <>No matches.</>
        )}
      </div>
    );
  }

  // Calculate aria-setsize/posinset per parent group.
  const groupCounts = new Map<string | null, FlatItem[]>();
  for (const it of visibleItems) {
    const arr = groupCounts.get(it.parentId) ?? [];
    arr.push(it);
    groupCounts.set(it.parentId, arr);
  }

  return (
    <div className="tree" role="tree" aria-label="Connection tree" tabIndex={-1}>
      {visibleItems.map((item) => {
        const { node, depth } = item;
        const selected = selectedNodeId === node.id;
        const isFocused = focusedId === node.id;
        const isFolder = node.kind === "folder";
        const collapsed = isFolder ? (node.collapsed ?? false) : undefined;
        const siblings = groupCounts.get(item.parentId) ?? [];
        const posinset = siblings.findIndex((s) => s.node.id === node.id) + 1;
        const setsize = siblings.length;

        return (
          <div
            key={node.id}
            ref={(el) => {
              itemRefs.current[node.id] = el;
            }}
            className={classNames("tree-node", { selected })}
            style={{ paddingLeft: 8 + depth * 12 }}
            role="treeitem"
            aria-level={depth + 1}
            aria-posinset={posinset}
            aria-setsize={setsize}
            aria-selected={selected}
            aria-expanded={isFolder ? !collapsed : undefined}
            tabIndex={isFocused ? 0 : -1}
            onFocus={() => setFocusedId(node.id)}
            onClick={() => {
              dispatch({ type: "tree/select", id: node.id });
              if (isFolder) {
                dispatch({ type: "tree/toggleCollapse", id: node.id });
              }
              focusItem(node.id);
            }}
            onDoubleClick={() => {
              if (!isFolder) void openConnection(node.id);
            }}
            onKeyDown={(e) => onKeyDown(e, item)}
            title={
              !isFolder
                ? `${node.protocol} ${node.host}:${node.port}`
                : undefined
            }
          >
            {isFolder ? (
              <span aria-hidden="true">
                {collapsed ? "▸" : "▾"} 📁{" "}
              </span>
            ) : (
              <span aria-hidden="true">🖥 </span>
            )}
            {editingId === node.id ? (
              <input
                autoFocus
                aria-label={`Rename ${node.name}`}
                value={editingValue}
                onChange={(e) => setEditingValue(e.target.value)}
                onBlur={commitRename}
                onKeyDown={(e) => {
                  e.stopPropagation();
                  if (e.key === "Enter") {
                    e.preventDefault();
                    commitRename();
                    focusItem(node.id);
                  } else if (e.key === "Escape") {
                    e.preventDefault();
                    setEditingId(null);
                    setEditingValue("");
                    focusItem(node.id);
                  }
                }}
                onClick={(e) => e.stopPropagation()}
              />
            ) : (
              <>
                {node.name}
                {!isFolder && (
                  <span
                    style={{
                      color: "var(--fg-muted)",
                      marginLeft: 6,
                      fontSize: 11,
                    }}
                  >
                    {node.protocol}
                  </span>
                )}
              </>
            )}
          </div>
        );
      })}
    </div>
  );
}
