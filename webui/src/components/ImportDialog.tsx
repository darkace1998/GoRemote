import { useState } from "react";
import { bridge } from "../bridge";
import { notify, useAppDispatch } from "../state/store";
import type { Warning, WarningSeverity } from "../types";
import { Modal } from "../a11y/Modal";

interface Props {
  onClose: () => void;
}

const SEVERITIES: WarningSeverity[] = ["error", "warning", "info"];

export function ImportDialog({ onClose }: Props) {
  const dispatch = useAppDispatch();
  const [fileName, setFileName] = useState<string | null>(null);
  const [warnings, setWarnings] = useState<Warning[] | null>(null);
  const [busy, setBusy] = useState(false);

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setFileName(file.name);
    setWarnings(null);
    setBusy(true);
    try {
      const contents = await file.text();
      const res = await bridge.importMRemoteNG(file.name, contents);
      setWarnings(res.warnings);
      const tree = await bridge.listConnections();
      dispatch({ type: "tree/set", tree });
      notify(
        dispatch,
        "info",
        `Imported ${res.importedCount} item(s) from ${file.name}`,
      );
    } catch (err) {
      notify(dispatch, "error", `Import failed: ${String(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const grouped: Record<WarningSeverity, Warning[]> = {
    info: [],
    warning: [],
    error: [],
  };
  (warnings ?? []).forEach((w) => grouped[w.severity].push(w));

  return (
    <Modal onClose={onClose} labelledBy="imp-title">
      <h2 id="imp-title">Import mRemoteNG</h2>
      <div className="row">
        <label htmlFor="imp-file">File</label>
        <input
          id="imp-file"
          type="file"
          accept=".xml,.csv"
          onChange={onFile}
          disabled={busy}
        />
      </div>
      {fileName && (
        <div style={{ color: "var(--fg-muted)", fontSize: 12 }}>
          {busy ? "Importing…" : `Last import: ${fileName}`}
        </div>
      )}
      {warnings && warnings.length === 0 && (
        <p style={{ color: "var(--fg-muted)" }}>No warnings.</p>
      )}
      {warnings &&
        SEVERITIES.filter((s) => grouped[s].length > 0).map((s) => (
          <div key={s} className="warning-group">
            <h4 style={{ textTransform: "capitalize" }}>
              {s} ({grouped[s].length})
            </h4>
            <ul>
              {grouped[s].map((w, i) => (
                <li key={`${s}-${i}`}>
                  <code>{w.code}</code>{" "}
                  {w.path && (
                    <span style={{ color: "var(--fg-muted)" }}>
                      @ {w.path}
                      {w.field ? `.${w.field}` : ""}
                    </span>
                  )}
                  {" — "}
                  {w.message}
                </li>
              ))}
            </ul>
          </div>
        ))}
      <div className="actions">
        <button type="button" onClick={onClose}>
          Close
        </button>
      </div>
    </Modal>
  );
}
