import {
  useEffect,
  useRef,
  type ReactNode,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { installFocusTrap } from "./focusTrap";

interface Props {
  children: ReactNode;
  /** id of the heading that labels the dialog. */
  labelledBy?: string;
  /** id of the descriptive text for the dialog. */
  describedBy?: string;
  /** Optional plain-string label when no heading id is available. */
  label?: string;
  onClose: () => void;
  /** Close on backdrop click. Default true. */
  closeOnBackdrop?: boolean;
  /** Close on Escape. Default true. */
  closeOnEscape?: boolean;
  className?: string;
}

/**
 * Accessible modal dialog: renders backdrop + dialog with role="dialog",
 * traps focus within, restores focus on unmount, and closes on Escape.
 */
export function Modal({
  children,
  labelledBy,
  describedBy,
  label,
  onClose,
  closeOnBackdrop = true,
  closeOnEscape = true,
  className,
}: Props) {
  const dialogRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const node = dialogRef.current;
    if (!node) return;
    const trap = installFocusTrap(node);
    return () => trap.release();
  }, []);

  const onKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (closeOnEscape && e.key === "Escape") {
      e.stopPropagation();
      onClose();
    }
  };

  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (closeOnBackdrop && e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        aria-label={!labelledBy ? label : undefined}
        className={className ? `modal ${className}` : "modal"}
        onKeyDown={onKeyDown}
      >
        {children}
      </div>
    </div>
  );
}
