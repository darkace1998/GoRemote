import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent } from "@testing-library/react";
import {
  formatCombo,
  isTypingTarget,
  keymap,
  matches,
  parseCombo,
} from "./keymap";

afterEach(() => {
  keymap.__resetForTesting();
  document.body.innerHTML = "";
});

describe("parseCombo", () => {
  it("parses simple combos", () => {
    expect(parseCombo("Ctrl+T")).toEqual({
      mods: { ctrl: true },
      modIsAny: false,
      key: "t",
    });
  });
  it("parses Mod alias", () => {
    const c = parseCombo("Mod+T");
    expect(c.modIsAny).toBe(true);
    expect(c.key).toBe("t");
  });
  it("parses chord-free single keys", () => {
    expect(parseCombo("F2").key).toBe("f2");
    expect(parseCombo("?").key).toBe("?");
  });
});

describe("matches", () => {
  it("requires exact modifier set for non-Mod combos", () => {
    const c = parseCombo("Ctrl+T");
    expect(
      matches(
        c,
        new KeyboardEvent("keydown", { key: "t", ctrlKey: true }),
      ),
    ).toBe(true);
    expect(
      matches(
        c,
        new KeyboardEvent("keydown", {
          key: "t",
          ctrlKey: true,
          shiftKey: true,
        }),
      ),
    ).toBe(false);
    expect(
      matches(c, new KeyboardEvent("keydown", { key: "t" })),
    ).toBe(false);
  });
  it("rejects mismatched key", () => {
    const c = parseCombo("Ctrl+T");
    expect(
      matches(
        c,
        new KeyboardEvent("keydown", { key: "x", ctrlKey: true }),
      ),
    ).toBe(false);
  });
});

describe("registry", () => {
  it("invokes registered handlers and respects unregister", () => {
    const handler = vi.fn();
    const off = keymap.register({
      id: "test-1",
      keys: "Ctrl+t",
      description: "test",
      handler,
    });

    fireEvent.keyDown(window, { key: "t", ctrlKey: true });
    expect(handler).toHaveBeenCalledTimes(1);

    off();
    fireEvent.keyDown(window, { key: "t", ctrlKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("does not fire when typing in an input by default", () => {
    const handler = vi.fn();
    keymap.register({
      id: "test-2",
      keys: "?",
      description: "help",
      handler,
    });

    const input = document.createElement("input");
    input.type = "text";
    document.body.appendChild(input);
    input.focus();

    fireEvent.keyDown(input, { key: "?" });
    expect(handler).not.toHaveBeenCalled();

    // Outside an input, it does fire.
    fireEvent.keyDown(document.body, { key: "?" });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("respects allowInInput=true", () => {
    const handler = vi.fn();
    keymap.register({
      id: "test-3",
      keys: "Ctrl+s",
      description: "save",
      allowInInput: true,
      handler,
    });
    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    fireEvent.keyDown(input, { key: "s", ctrlKey: true });
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("matches first registered binding only", () => {
    const a = vi.fn();
    const b = vi.fn();
    keymap.register({ id: "a", keys: "F2", description: "a", handler: a });
    keymap.register({ id: "b", keys: "F2", description: "b", handler: b });
    fireEvent.keyDown(window, { key: "F2" });
    expect(a).toHaveBeenCalledTimes(1);
    expect(b).not.toHaveBeenCalled();
  });
});

describe("isTypingTarget", () => {
  it("treats text inputs as typing targets", () => {
    const i = document.createElement("input");
    i.type = "text";
    expect(isTypingTarget(i)).toBe(true);
  });
  it("ignores button-typed inputs", () => {
    const i = document.createElement("input");
    i.type = "button";
    expect(isTypingTarget(i)).toBe(false);
  });
  it("treats textareas as typing", () => {
    expect(isTypingTarget(document.createElement("textarea"))).toBe(true);
  });
});

describe("formatCombo", () => {
  it("formats Mod for non-Mac platforms", () => {
    // Default jsdom platform is not Mac.
    expect(formatCombo("Mod+T")).toContain("T");
  });
});
