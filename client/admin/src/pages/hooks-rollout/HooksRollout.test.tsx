import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminHooksRollout } from "@/lib/gramAdminApi";
import { HooksRollout } from "@/pages/hooks-rollout/HooksRollout";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getHooksRollout: vi.fn(),
  setHooksRolloutDefault: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getHooksRollout: mocks.getHooksRollout,
    setHooksRolloutDefault: mocks.setHooksRolloutDefault,
  };
});

const UNPINNED: AdminHooksRollout = {
  current_version: 46,
  canary_organization_slugs: ["canary-org"],
  overrides: [],
  recent_changes: [],
};

const PINNED: AdminHooksRollout = {
  current_version: 46,
  default_pin: {
    version: 44,
    set_by: "operator@example.com",
    set_at: "2026-01-02T00:00:00Z",
  },
  canary_organization_slugs: ["canary-org"],
  overrides: [
    {
      organization_id: "org_held",
      organization_name: "Held Org",
      organization_slug: "held-org",
      pin: {
        version: 40,
        set_by: "operator@example.com",
        set_at: "2026-01-01T00:00:00Z",
      },
    },
  ],
  recent_changes: [
    {
      version: 44,
      set_by: "operator@example.com",
      set_at: "2026-01-02T00:00:00Z",
    },
    {
      organization_id: "org_cleared",
      organization_slug: "cleared-org",
      set_by: "operator@example.com",
      set_at: "2026-01-01T00:00:00Z",
    },
  ],
};

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("HooksRollout", () => {
  it("explains that the legacy flag decides until a default pin is set", async () => {
    mocks.getHooksRollout.mockResolvedValue(UNPINNED);
    await renderWithApp(<HooksRollout />);

    expect(
      await screen.findByText(
        /still follow the legacy hooks-rollout PostHog flag/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("Not set")).toBeTruthy();
    expect(screen.getByText(/canary-org/)).toBeTruthy();
    expect(screen.getByText("No overrides.")).toBeTruthy();
    expect(screen.getByText("No pin has been set yet.")).toBeTruthy();
    expect(
      (screen.getByLabelText("Default hooks version pin") as HTMLInputElement)
        .value,
    ).toBe("46");
  });

  it("shows how far the default pin and overrides are behind", async () => {
    mocks.getHooksRollout.mockResolvedValue(PINNED);
    await renderWithApp(<HooksRollout />);

    expect(
      await screen.findByText(/Hooks version 46 has not been rolled out/),
    ).toBeTruthy();
    expect(screen.getByText("2 behind")).toBeTruthy();
    expect(screen.getByText("6 behind")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Held Org" })).toBeTruthy();
    expect(screen.getByText("Pinned to 44")).toBeTruthy();
    expect(screen.getByText("Default pin", { selector: "td" })).toBeTruthy();
    expect(screen.getByText("Cleared override")).toBeTruthy();
    expect(screen.getByRole("link", { name: "cleared-org" })).toBeTruthy();
  });

  it("sets the default pin only after confirmation", async () => {
    mocks.getHooksRollout.mockResolvedValue(PINNED);
    const updated: AdminHooksRollout = {
      ...PINNED,
      default_pin: { ...PINNED.default_pin!, version: 46 },
    };
    mocks.setHooksRolloutDefault.mockImplementation(async () => {
      mocks.getHooksRollout.mockResolvedValue(updated);
      return updated;
    });
    await renderWithApp(<HooksRollout />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Set default pin" }),
    );
    expect(
      await screen.findByText(
        /Every organization without an override will receive hooks version 46/,
      ),
    ).toBeTruthy();
    expect(mocks.setHooksRolloutDefault).not.toHaveBeenCalled();

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      Array.from(dialog.querySelectorAll("button")).find(
        (button) => button.textContent === "Set default pin",
      )!,
    );
    await waitFor(() =>
      expect(mocks.setHooksRolloutDefault).toHaveBeenCalledWith(46),
    );
    expect(
      await screen.findByText(
        "Every organization without an override is cleared for hooks version 46.",
      ),
    ).toBeTruthy();
  });

  it("does not send the default pin when the dialog is cancelled", async () => {
    mocks.getHooksRollout.mockResolvedValue(PINNED);
    await renderWithApp(<HooksRollout />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Set default pin" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mocks.setHooksRolloutDefault).not.toHaveBeenCalled();
  });

  it("refuses a pin above the current version", async () => {
    mocks.getHooksRollout.mockResolvedValue(PINNED);
    await renderWithApp(<HooksRollout />);

    fireEvent.change(
      await screen.findByLabelText("Default hooks version pin"),
      {
        target: { value: "47" },
      },
    );
    expect(
      screen.getByText("The pin must be a whole number from 1 to 46."),
    ).toBeTruthy();
    expect(
      (
        screen.getByRole("button", {
          name: "Set default pin",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });
});
