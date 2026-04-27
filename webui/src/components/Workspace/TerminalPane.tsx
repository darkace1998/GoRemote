import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { bridge } from "../../bridge";
import type { SessionHandle } from "../../types";

interface Props {
  handle: SessionHandle;
  visible: boolean;
}

export function TerminalPane({ handle, visible }: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  useEffect(() => {
    if (!hostRef.current) return;
    const term = new Terminal({
      cursorBlink: true,
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
      fontSize: 13,
      theme: { background: "#000000" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current);
    try {
      fit.fit();
    } catch {
      // element may not yet be sized
    }

    const inputDisposer = term.onData((data) => {
      void bridge.sendInput(handle, data);
    });
    const unsubscribeOutput = bridge.subscribeOutput(handle, (chunk) => {
      term.write(chunk);
    });

    const host = hostRef.current;
    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
        void bridge.resize(handle, term.cols, term.rows);
      } catch {
        // ignore transient sizing errors
      }
    });
    ro.observe(host);

    termRef.current = term;
    fitRef.current = fit;

    return () => {
      ro.disconnect();
      inputDisposer.dispose();
      unsubscribeOutput();
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
  }, [handle]);

  useEffect(() => {
    if (visible && fitRef.current && termRef.current) {
      try {
        fitRef.current.fit();
        void bridge.resize(
          handle,
          termRef.current.cols,
          termRef.current.rows,
        );
      } catch {
        // ignore
      }
      termRef.current.focus();
    }
  }, [visible, handle]);

  return (
    <div
      className="terminal-host"
      ref={hostRef}
      style={{ display: visible ? "block" : "none" }}
      role="region"
      aria-label="Terminal"
    />
  );
}
