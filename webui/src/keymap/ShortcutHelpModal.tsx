import { Modal } from "../a11y/Modal";
import { keymap, formatCombo } from "./keymap";

interface Props {
  onClose: () => void;
}

export function ShortcutHelpModal({ onClose }: Props) {
  const bindings = keymap.list();
  const groups = new Map<string, typeof bindings>();
  for (const b of bindings) {
    const g = b.group ?? "General";
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g)!.push(b);
  }

  return (
    <Modal
      onClose={onClose}
      labelledBy="shortcut-help-title"
      describedBy="shortcut-help-desc"
    >
      <h2 id="shortcut-help-title">Keyboard Shortcuts</h2>
      <p id="shortcut-help-desc" className="muted">
        Press <kbd>Esc</kbd> to close. Shortcuts using <kbd>Mod</kbd> are
        <kbd>Ctrl</kbd> on Windows/Linux and <kbd>⌘</kbd> on macOS.
      </p>
      {[...groups.entries()].map(([group, items]) => (
        <section key={group} className="shortcut-group">
          <h3>{group}</h3>
          <table className="shortcut-table">
            <tbody>
              {items.map((b) => (
                <tr key={b.id}>
                  <td className="shortcut-keys">
                    {(Array.isArray(b.keys) ? b.keys : [b.keys])
                      .map(formatCombo)
                      .map((label, i) => (
                        <kbd key={i}>{label}</kbd>
                      ))}
                  </td>
                  <td>{b.description}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ))}
      <div className="actions">
        <button type="button" onClick={onClose} autoFocus>
          Close
        </button>
      </div>
    </Modal>
  );
}
