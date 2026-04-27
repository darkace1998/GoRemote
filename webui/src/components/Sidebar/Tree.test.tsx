import { afterEach, describe, expect, it, vi } from "vitest";
import { useEffect } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { Tree } from "./Tree";
import { AppStateProvider, useAppDispatch } from "../../state/store";
import type { TreeNode } from "../../types";

vi.mock("../../bridge", () => ({
  bridge: {
    listConnections: () => Promise.resolve([]),
    openSession: vi.fn().mockResolvedValue("h1"),
  },
}));

afterEach(() => {
  document.body.innerHTML = "";
});

const sample: TreeNode[] = [
  {
    kind: "folder",
    id: "f1",
    name: "Folder One",
    collapsed: false,
    children: [
      {
        kind: "connection",
        id: "c1",
        name: "Conn One",
        protocol: "ssh",
        host: "h",
        port: 22,
      },
      {
        kind: "connection",
        id: "c2",
        name: "Conn Two",
        protocol: "ssh",
        host: "h",
        port: 22,
      },
    ],
  },
  {
    kind: "connection",
    id: "c3",
    name: "Conn Three",
    protocol: "telnet",
    host: "h",
    port: 23,
  },
];

function Seed({ tree }: { tree: TreeNode[] }) {
  const dispatch = useAppDispatch();
  useEffect(() => {
    // Wait one macrotask so the AppStateProvider's initial listConnections()
    // promise resolves first; otherwise its empty result clobbers ours.
    const t = setTimeout(() => dispatch({ type: "tree/set", tree }), 0);
    return () => clearTimeout(t);
  }, [dispatch, tree]);
  return null;
}

async function setup() {
  const result = render(
    <AppStateProvider>
      <Seed tree={sample} />
      <Tree />
    </AppStateProvider>,
  );
  // Allow the provider's initial load + the seed's setTimeout to flush.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 5));
  });
  return result;
}

describe("Tree keyboard navigation", () => {
  it("renders treeitems with ARIA level/expanded", async () => {
    await setup();
    const items = screen.getAllByRole("treeitem");
    expect(items.length).toBeGreaterThan(0);
    const folder = items.find((i) => i.textContent?.includes("Folder One"));
    expect(folder).toHaveAttribute("aria-expanded", "true");
    expect(folder).toHaveAttribute("aria-level", "1");
  });

  it("ArrowDown/Up move selection between visible items", async () => {
    await setup();
    const items = () => screen.getAllByRole("treeitem");
    const folder = items()[0];
    folder.focus();
    fireEvent.keyDown(folder, { key: "ArrowDown" });
    await act(async () => {
      await Promise.resolve();
    });
    const after = items();
    const second = after[1];
    expect(second).toHaveAttribute("aria-selected", "true");

    fireEvent.keyDown(second, { key: "ArrowUp" });
    await act(async () => {
      await Promise.resolve();
    });
    expect(items()[0]).toHaveAttribute("aria-selected", "true");
  });

  it("ArrowLeft collapses an expanded folder, ArrowRight re-expands", async () => {
    await setup();
    const items = () => screen.getAllByRole("treeitem");
    const folder = items()[0];
    folder.focus();
    expect(folder).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(folder, { key: "ArrowLeft" });
    await act(async () => {
      await Promise.resolve();
    });
    // After collapsing, children should not be in the DOM.
    expect(items().length).toBe(2); // Folder + Conn Three
    const folderAfter = items()[0];
    expect(folderAfter).toHaveAttribute("aria-expanded", "false");

    fireEvent.keyDown(folderAfter, { key: "ArrowRight" });
    await act(async () => {
      await Promise.resolve();
    });
    expect(items().length).toBe(4); // re-expanded
    expect(items()[0]).toHaveAttribute("aria-expanded", "true");
  });

  it("Enter on a folder toggles expansion", async () => {
    await setup();
    const items = () => screen.getAllByRole("treeitem");
    const folder = items()[0];
    folder.focus();
    fireEvent.keyDown(folder, { key: "Enter" });
    await act(async () => {
      await Promise.resolve();
    });
    expect(items()[0]).toHaveAttribute("aria-expanded", "false");
  });
});
