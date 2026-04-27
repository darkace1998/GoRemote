import { afterEach, describe, expect, it, vi } from "vitest";
import { useEffect } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { Tabs } from "./Tabs";
import { AppStateProvider, useAppDispatch } from "../../state/store";
import type { Tab } from "../../types";

vi.mock("../../bridge", () => ({
  bridge: {
    listConnections: () => Promise.resolve([]),
    closeSession: vi.fn(),
  },
}));

afterEach(() => {
  document.body.innerHTML = "";
});

function Seeder({ tabs }: { tabs: Tab[] }) {
  const dispatch = useAppDispatch();
  useEffect(() => {
    for (const t of tabs) dispatch({ type: "tabs/open", tab: t });
  }, [dispatch, tabs]);
  return null;
}

const sample: Tab[] = [
  { id: "a", title: "Alpha", sessionHandle: "a", protocol: "ssh" },
  { id: "b", title: "Bravo", sessionHandle: "b", protocol: "ssh" },
  { id: "c", title: "Charlie", sessionHandle: "c", protocol: "ssh" },
];

describe("Tabs (tablist)", () => {
  it("renders tabs with proper ARIA wiring", async () => {
    render(
      <AppStateProvider>
        <Seeder tabs={sample} />
        <Tabs />
      </AppStateProvider>,
    );

    await act(async () => {
      await Promise.resolve();
    });

    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(3);
    expect(tabs[0]).toHaveAttribute("aria-controls", "tabpanel-a");
    // Last opened becomes active.
    expect(tabs[2]).toHaveAttribute("aria-selected", "true");
  });

  it("ArrowRight moves aria-selected to the next tab", async () => {
    render(
      <AppStateProvider>
        <Seeder tabs={sample} />
        <Tabs />
      </AppStateProvider>,
    );
    await act(async () => {
      await Promise.resolve();
    });

    const tabs = () => screen.getAllByRole("tab");
    // The store activates the *last* opened tab on each open. Click the first
    // to make it active before driving arrow keys.
    fireEvent.click(tabs()[0]);
    expect(tabs()[0]).toHaveAttribute("aria-selected", "true");

    fireEvent.keyDown(tabs()[0], { key: "ArrowRight" });
    expect(tabs()[1]).toHaveAttribute("aria-selected", "true");
    expect(tabs()[0]).toHaveAttribute("aria-selected", "false");

    fireEvent.keyDown(tabs()[1], { key: "ArrowLeft" });
    expect(tabs()[0]).toHaveAttribute("aria-selected", "true");
  });

  it("Home/End jump to first/last tab", async () => {
    render(
      <AppStateProvider>
        <Seeder tabs={sample} />
        <Tabs />
      </AppStateProvider>,
    );
    await act(async () => {
      await Promise.resolve();
    });
    const tabs = () => screen.getAllByRole("tab");
    fireEvent.click(tabs()[1]);
    fireEvent.keyDown(tabs()[1], { key: "Home" });
    expect(tabs()[0]).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(tabs()[0], { key: "End" });
    expect(tabs()[2]).toHaveAttribute("aria-selected", "true");
  });
});
