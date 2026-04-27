import classNames from "classnames";
import { useRef } from "react";
import { bridge } from "../../bridge";
import { useAppDispatch, useAppState } from "../../state/store";

/**
 * Accessible tab list. Implements the WAI-ARIA Authoring Practices "Tabs with
 * Manual Activation" pattern with roving tabindex: only the active tab is in
 * the focus order; arrow keys move selection (and activation) between tabs.
 */
export function Tabs() {
  const { tabs, activeTabId } = useAppState();
  const dispatch = useAppDispatch();
  const tabRefs = useRef<Record<string, HTMLDivElement | null>>({});

  if (tabs.length === 0) {
    return <div className="tabs" role="tablist" aria-label="Open sessions" />;
  }

  const focusTab = (id: string) => {
    const el = tabRefs.current[id];
    el?.focus();
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>, idx: number) => {
    let target: number | null = null;
    switch (e.key) {
      case "ArrowRight":
        target = (idx + 1) % tabs.length;
        break;
      case "ArrowLeft":
        target = (idx - 1 + tabs.length) % tabs.length;
        break;
      case "Home":
        target = 0;
        break;
      case "End":
        target = tabs.length - 1;
        break;
      case "Delete": {
        e.preventDefault();
        const t = tabs[idx];
        void bridge.closeSession(t.id);
        dispatch({ type: "tabs/close", id: t.id });
        return;
      }
      case "Enter":
      case " ":
        e.preventDefault();
        dispatch({ type: "tabs/activate", id: tabs[idx].id });
        return;
      default:
        return;
    }
    if (target != null) {
      e.preventDefault();
      const next = tabs[target];
      dispatch({ type: "tabs/activate", id: next.id });
      focusTab(next.id);
    }
  };

  return (
    <div className="tabs" role="tablist" aria-label="Open sessions">
      {tabs.map((t, i) => {
        const active = t.id === activeTabId;
        return (
          <div
            key={t.id}
            ref={(el) => {
              tabRefs.current[t.id] = el;
            }}
            id={`tab-${t.id}`}
            role="tab"
            aria-selected={active}
            aria-controls={`tabpanel-${t.id}`}
            tabIndex={active ? 0 : -1}
            className={classNames("tab", { active })}
            onClick={() => dispatch({ type: "tabs/activate", id: t.id })}
            onKeyDown={(e) => onKeyDown(e, i)}
          >
            <span>{t.title}</span>
            <button
              className="close"
              tabIndex={-1}
              aria-label={`Close ${t.title}`}
              onClick={(e) => {
                e.stopPropagation();
                void bridge.closeSession(t.id);
                dispatch({ type: "tabs/close", id: t.id });
              }}
            >
              ×
            </button>
          </div>
        );
      })}
    </div>
  );
}
