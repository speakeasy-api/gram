import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminHooksReleaseProgress } from "@/lib/gramAdminApi";
import { ReleaseProgress } from "@/pages/hooks-rollout/ReleaseProgress";
import { renderWithApp } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getHooksReleaseProgress: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return {
    ...actual,
    getHooksReleaseProgress: mocks.getHooksReleaseProgress,
  };
});

const PROGRESS: AdminHooksReleaseProgress = {
  current_version: 66,
  current_binary_version: "0.3.58",
  organizations: 4,
  organizations_cleared: 1,
  organizations_held: 2,
  organizations_on_legacy_flag: 1,
  repos: 5,
  repos_on_current: 2,
  repo_versions: [
    { version: "66", repos: 2 },
    { version: "64", repos: 2 },
    { version: "", repos: 1 },
  ],
  fleet: {
    window_days: 7,
    installs: 10,
    installs_on_current: 4,
    versions: [
      { version: "0.3.58", status: "current", installs: 4 },
      { version: "0.3.40", status: "behind", installs: 2 },
      { version: "", status: "unsupported", installs: 4 },
    ],
  },
};

beforeEach(() => {
  mocks.getHooksReleaseProgress.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ReleaseProgress", () => {
  it("shows how far the current release has reached", async () => {
    mocks.getHooksReleaseProgress.mockResolvedValue(PROGRESS);
    await renderWithApp(<ReleaseProgress />);

    expect(
      await screen.findByText("Hooks version 66, binary 0.3.58."),
    ).toBeTruthy();
    expect(screen.getByText("1 of 4")).toBeTruthy();
    expect(screen.getByText("2 of 5")).toBeTruthy();
    expect(screen.getByText("4 of 10")).toBeTruthy();
    expect(screen.getByText(/2 held below it by a pin/)).toBeTruthy();
    expect(screen.getByText(/1 follow the legacy PostHog flag/)).toBeTruthy();
    expect(screen.getByText("Never published")).toBeTruthy();
    expect(screen.getByText("Legacy hooks")).toBeTruthy();
    expect(screen.getAllByText("Current")).toHaveLength(2);
  });

  it("keeps repository counts when install data is unavailable", async () => {
    mocks.getHooksReleaseProgress.mockResolvedValue({
      ...PROGRESS,
      fleet: undefined,
    });
    await renderWithApp(<ReleaseProgress />);

    expect(await screen.findByText("2 of 5")).toBeTruthy();
    expect(screen.getByText("Install counts are unavailable.")).toBeTruthy();
    expect(screen.queryByText("Installs by binary version")).toBeNull();
  });

  it("offers a retry when the progress fails to load", async () => {
    mocks.getHooksReleaseProgress.mockRejectedValue(new Error("boom"));
    await renderWithApp(<ReleaseProgress />);

    expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
