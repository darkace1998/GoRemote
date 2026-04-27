import { useEffect } from "react";
import classNames from "classnames";
import { useAppDispatch, useAppState } from "../../state/store";

export function ToastContainer() {
  const { notifications } = useAppState();
  const dispatch = useAppDispatch();

  useEffect(() => {
    const timers = notifications
      .filter((n) => n.timeoutMs && n.timeoutMs > 0)
      .map((n) =>
        setTimeout(
          () => dispatch({ type: "notify/dismiss", id: n.id }),
          n.timeoutMs,
        ),
      );
    return () => timers.forEach((t) => clearTimeout(t));
  }, [notifications, dispatch]);

  if (notifications.length === 0) return null;

  return (
    <div className="toasts" role="region" aria-live="polite" aria-label="Notifications">
      {notifications.map((n) => (
        <div key={n.id} className={classNames("toast", n.severity)} role="status">
          <span>{n.message}</span>
          <button
            className="dismiss"
            aria-label="Dismiss notification"
            onClick={() => dispatch({ type: "notify/dismiss", id: n.id })}
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
