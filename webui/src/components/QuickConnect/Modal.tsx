import { useState } from "react";
import { bridge } from "../../bridge";
import { notify, useAppDispatch } from "../../state/store";
import { PROTOCOLS, type Protocol } from "../../types";
import { Modal } from "../../a11y/Modal";

const DEFAULT_PORT: Record<Protocol, number> = {
  ssh: 22,
  telnet: 23,
  raw: 23,
  rlogin: 513,
};

interface Props {
  onClose: () => void;
}

export function QuickConnectModal({ onClose }: Props) {
  const dispatch = useAppDispatch();
  const [protocol, setProtocol] = useState<Protocol>("ssh");
  const [host, setHost] = useState("");
  const [port, setPort] = useState<number>(DEFAULT_PORT.ssh);
  const [username, setUsername] = useState("");
  const [busy, setBusy] = useState(false);
  const [hostError, setHostError] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!host) {
      setHostError("Host is required");
      return;
    }
    setHostError(null);
    setBusy(true);
    try {
      const handle = await bridge.quickConnect({
        protocol,
        host,
        port,
        username: username || undefined,
      });
      dispatch({
        type: "tabs/open",
        tab: {
          id: handle,
          title: `${protocol}://${host}`,
          sessionHandle: handle,
          protocol,
        },
      });
      onClose();
    } catch (err) {
      notify(dispatch, "error", `Quick connect failed: ${String(err)}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal onClose={onClose} labelledBy="qc-title">
      <form onSubmit={submit}>
        <h2 id="qc-title">Quick Connect</h2>
        <div className="row">
          <label htmlFor="qc-protocol">Protocol</label>
          <select
            id="qc-protocol"
            value={protocol}
            onChange={(e) => {
              const p = e.target.value as Protocol;
              setProtocol(p);
              setPort(DEFAULT_PORT[p]);
            }}
          >
            {PROTOCOLS.map((p) => (
              <option key={p} value={p}>
                {p.toUpperCase()}
              </option>
            ))}
          </select>
        </div>
        <div className="row">
          <label htmlFor="qc-host">
            Host <span aria-hidden="true">*</span>
          </label>
          <input
            id="qc-host"
            autoFocus
            required
            aria-required="true"
            aria-invalid={hostError ? true : undefined}
            aria-describedby={hostError ? "qc-host-err" : undefined}
            value={host}
            onChange={(e) => {
              setHost(e.target.value);
              if (hostError) setHostError(null);
            }}
            placeholder="example.com"
          />
        </div>
        {hostError && (
          <div id="qc-host-err" role="alert" className="form-error">
            {hostError}
          </div>
        )}
        <div className="row">
          <label htmlFor="qc-port">Port</label>
          <input
            id="qc-port"
            type="number"
            min={1}
            max={65535}
            value={port}
            onChange={(e) => setPort(Number(e.target.value))}
          />
        </div>
        <div className="row">
          <label htmlFor="qc-user">Username</label>
          <input
            id="qc-user"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="optional"
          />
        </div>
        <div className="actions">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="primary" disabled={busy || !host}>
            {busy ? "Connecting…" : "Connect"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
