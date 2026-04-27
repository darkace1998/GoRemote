import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SettingsView } from "./SettingsView";
import {
  __setSettingsAPIForTesting,
  defaultSettings,
  type Settings,
  type SettingsAPI,
} from "../../api/settings";

function makeAPI(overrides?: Partial<SettingsAPI>): {
  api: SettingsAPI;
  get: ReturnType<typeof vi.fn>;
  update: ReturnType<typeof vi.fn>;
  saved: { value: Settings };
} {
  const saved = { value: defaultSettings() };
  const get = vi.fn(async () => ({ ...saved.value }));
  const update = vi.fn(async (next: Settings) => {
    saved.value = { ...next, updatedAt: "2024-01-01T00:00:00Z" };
    return { ...saved.value };
  });
  const api: SettingsAPI = {
    get: overrides?.get ?? get,
    update: overrides?.update ?? update,
  };
  return { api, get, update, saved };
}

describe("SettingsView", () => {
  let prevAPI: SettingsAPI | null = null;

  beforeEach(() => {
    document.documentElement.removeAttribute("data-theme");
    document.body.removeAttribute("data-theme");
  });

  afterEach(() => {
    __setSettingsAPIForTesting(prevAPI);
    prevAPI = null;
  });

  it("renders defaults from the API on mount", async () => {
    const { api } = makeAPI();
    prevAPI = __setSettingsAPIForTesting(api);

    render(<SettingsView />);

    await waitFor(() => {
      expect(screen.getByLabelText("Theme")).toHaveValue("system");
    });
    expect(screen.getByLabelText("Font size")).toHaveValue(13);
    expect(screen.getByLabelText("Confirm on close")).toBeChecked();
    expect(screen.getByLabelText("Auto reconnect")).not.toBeChecked();
    expect(screen.getByLabelText("Telemetry")).not.toBeChecked();
    expect(screen.getByTestId("settings-dirty-indicator")).toHaveTextContent(
      "Saved",
    );
  });

  it("activates Save on dirty edit and persists via api.update", async () => {
    const { api, update } = makeAPI();
    prevAPI = __setSettingsAPIForTesting(api);
    const user = userEvent.setup();

    render(<SettingsView />);
    await waitFor(() => screen.getByLabelText("Theme"));

    const saveBtn = screen.getByRole("button", { name: "Save settings" });
    expect(saveBtn).toBeDisabled();

    const fontSize = screen.getByLabelText("Font size");
    await user.clear(fontSize);
    await user.type(fontSize, "18");

    expect(screen.getByTestId("settings-dirty-indicator")).toHaveTextContent(
      "Unsaved changes",
    );
    expect(saveBtn).toBeEnabled();

    await user.click(saveBtn);

    await waitFor(() => expect(update).toHaveBeenCalledTimes(1));
    expect(update.mock.calls[0][0]).toMatchObject({ fontSizePx: 18 });

    await waitFor(() =>
      expect(screen.getByTestId("settings-dirty-indicator")).toHaveTextContent(
        "Saved",
      ),
    );
  });

  it("surfaces backend validation errors inline", async () => {
    const { api } = makeAPI({
      update: vi.fn(async () => {
        throw new Error("invalid theme \"neon\"");
      }),
    });
    prevAPI = __setSettingsAPIForTesting(api);
    const user = userEvent.setup();

    render(<SettingsView />);
    await waitFor(() => screen.getByLabelText("Theme"));

    // Touch a field so Save becomes enabled.
    const fontSize = screen.getByLabelText("Font size");
    await user.clear(fontSize);
    await user.type(fontSize, "20");

    await user.click(screen.getByRole("button", { name: "Save settings" }));

    await waitFor(() =>
      expect(screen.getByTestId("settings-error")).toHaveTextContent(
        /invalid theme/,
      ),
    );
  });

  it("flips data-theme when the theme select changes", async () => {
    const { api } = makeAPI();
    prevAPI = __setSettingsAPIForTesting(api);
    const user = userEvent.setup();

    render(<SettingsView />);
    await waitFor(() => screen.getByLabelText("Theme"));

    await user.selectOptions(screen.getByLabelText("Theme"), "dark");

    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");

    await user.selectOptions(screen.getByLabelText("Theme"), "light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("Revert restores last-saved values and disables Save", async () => {
    const { api } = makeAPI();
    prevAPI = __setSettingsAPIForTesting(api);
    const user = userEvent.setup();

    render(<SettingsView />);
    await waitFor(() => screen.getByLabelText("Theme"));

    const fontSize = screen.getByLabelText("Font size");
    await user.clear(fontSize);
    await user.type(fontSize, "22");
    expect(screen.getByRole("button", { name: "Save settings" })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Revert" }));

    expect(screen.getByLabelText("Font size")).toHaveValue(13);
    expect(screen.getByRole("button", { name: "Save settings" })).toBeDisabled();
  });

  it("gates reconnect inputs on the auto-reconnect toggle", async () => {
    const { api } = makeAPI();
    prevAPI = __setSettingsAPIForTesting(api);
    const user = userEvent.setup();

    render(<SettingsView />);
    await waitFor(() => screen.getByLabelText("Theme"));

    expect(screen.getByLabelText("Reconnect max attempts")).toBeDisabled();
    expect(screen.getByLabelText("Reconnect delay ms")).toBeDisabled();

    await user.click(screen.getByLabelText("Auto reconnect"));

    expect(screen.getByLabelText("Reconnect max attempts")).toBeEnabled();
    expect(screen.getByLabelText("Reconnect delay ms")).toBeEnabled();
  });
});
