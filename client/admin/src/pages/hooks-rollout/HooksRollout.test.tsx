import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminHooksRollout } from "@/lib/gramAdminApi";
import { HooksRollout } from "@/pages/hooks-rollout/HooksRollout";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getHooksRollout: vi.fn(),
  setHooksRolloutDefault: vi.fn(),
  startHooksRolloutStage: vi.fn(),
  pauseHooksRolloutStage: vi.fn(),
  resumeHooksRolloutStage: vi.fn(),
  cancelHooksRolloutStage: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getHooksRollout: mocks.getHooksRollout,
    setHooksRolloutDefault: mocks.setHooksRolloutDefault,
    startHooksRolloutStage: mocks.startHooksRolloutStage,
    pauseHooksRolloutStage: mocks.pauseHooksRolloutStage,
    resumeHooksRolloutStage: mocks.resumeHooksRolloutStage,
    cancelHooksRolloutStage: mocks.cancelHooksRolloutStage,
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

const RUNNING: NonNullable<AdminHooksRollout["stage"]> = {
  version: 46,
  percent: 25,
  state: "running",
  set_by: "hooks-rollout-schedule",
  set_at: "2026-01-03T00:00:00Z",
  next_advance_at: "2026-01-04T00:00:00Z",
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
      within(dialog).getByRole("button", { name: "Set default pin" }),
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
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
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

  it("starts a staged rollout only after confirmation", async () => {
    mocks.getHooksRollout.mockResolvedValue(PINNED);
    const updated: AdminHooksRollout = { ...PINNED, stage: RUNNING };
    mocks.startHooksRolloutStage.mockImplementation(async () => {
      mocks.getHooksRollout.mockResolvedValue(updated);
      return updated;
    });
    await renderWithApp(<HooksRollout />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Start staged rollout" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(mocks.startHooksRolloutStage).not.toHaveBeenCalled();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Start rollout" }),
    );
    await waitFor(() =>
      expect(mocks.startHooksRolloutStage).toHaveBeenCalledWith(46),
    );
    expect(await screen.findByText("Running")).toBeTruthy();
  });

  it("refuses to stage a version at or below the default pin", async () => {
    mocks.getHooksRollout.mockResolvedValue(PINNED);
    await renderWithApp(<HooksRollout />);

    fireEvent.change(
      await screen.findByLabelText("Hooks version to roll out"),
      { target: { value: "44" } },
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Start staged rollout",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
  });

  it("has nothing to stage when the default pin is current", async () => {
    mocks.getHooksRollout.mockResolvedValue({
      ...PINNED,
      default_pin: { ...PINNED.default_pin!, version: 46 },
    });
    await renderWithApp(<HooksRollout />);

    expect(await screen.findByText(/Nothing to roll out/)).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Start staged rollout" }),
    ).toBeNull();
  });

  it("pauses a running rollout with a reason", async () => {
    mocks.getHooksRollout.mockResolvedValue({ ...PINNED, stage: RUNNING });
    const paused: AdminHooksRollout = {
      ...PINNED,
      stage: {
        ...RUNNING,
        state: "paused",
        reason: "hooks error alert",
        next_advance_at: undefined,
      },
    };
    mocks.pauseHooksRolloutStage.mockImplementation(async () => {
      mocks.getHooksRollout.mockResolvedValue(paused);
      return paused;
    });
    await renderWithApp(<HooksRollout />);

    const pause = (await screen.findByRole("button", {
      name: "Pause",
    })) as HTMLButtonElement;
    expect(pause.disabled).toBe(true);
    expect(screen.getByText("25%").getAttribute("aria-current")).toBe("step");

    fireEvent.change(
      screen.getByLabelText("Reason for pausing or cancelling"),
      { target: { value: "hooks error alert" } },
    );
    fireEvent.click(pause);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Pause rollout" }),
    );
    await waitFor(() =>
      expect(mocks.pauseHooksRolloutStage).toHaveBeenCalledWith(
        "hooks error alert",
      ),
    );
    expect(await screen.findByRole("button", { name: "Resume" })).toBeTruthy();
    expect(screen.getByText("hooks error alert")).toBeTruthy();
  });

  it("resumes a paused rollout", async () => {
    mocks.getHooksRollout.mockResolvedValue({
      ...PINNED,
      stage: {
        ...RUNNING,
        state: "paused",
        reason: "alert",
        next_advance_at: undefined,
      },
    });
    const resumed: AdminHooksRollout = {
      ...PINNED,
      stage: RUNNING,
    };
    mocks.resumeHooksRolloutStage.mockImplementation(async () => {
      mocks.getHooksRollout.mockResolvedValue(resumed);
      return resumed;
    });
    await renderWithApp(<HooksRollout />);

    fireEvent.click(await screen.findByRole("button", { name: "Resume" }));
    expect(mocks.resumeHooksRolloutStage).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Resume rollout" }),
    );
    await waitFor(() =>
      expect(mocks.resumeHooksRolloutStage).toHaveBeenCalled(),
    );
    expect(await screen.findByRole("button", { name: "Pause" })).toBeTruthy();
    expect(screen.queryByText("Paused")).toBeNull();
  });

  it("cancels a rollout only with a reason and after confirmation", async () => {
    mocks.getHooksRollout.mockResolvedValue({ ...PINNED, stage: RUNNING });
    const cancelled: AdminHooksRollout = {
      ...PINNED,
      stage: { ...RUNNING, state: "cancelled", reason: "bad release" },
    };
    mocks.cancelHooksRolloutStage.mockImplementation(async () => {
      mocks.getHooksRollout.mockResolvedValue(cancelled);
      return cancelled;
    });
    await renderWithApp(<HooksRollout />);

    const cancel = (await screen.findByRole("button", {
      name: "Cancel rollout",
    })) as HTMLButtonElement;
    expect(cancel.disabled).toBe(true);

    fireEvent.change(
      screen.getByLabelText("Reason for pausing or cancelling"),
      { target: { value: "bad release" } },
    );
    fireEvent.click(cancel);
    const dialog = await screen.findByRole("dialog");
    expect(mocks.cancelHooksRolloutStage).not.toHaveBeenCalled();
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Cancel rollout" }),
    );
    await waitFor(() =>
      expect(mocks.cancelHooksRolloutStage).toHaveBeenCalledWith("bad release"),
    );
    expect(await screen.findByText(/was cancelled by/)).toBeTruthy();
  });
});
