import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RestoreSkillVersionDialog } from "./RestoreSkillVersionDialog";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  restore: { mutateAsync: vi.fn(), isPending: false },
  invalidate: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => testState.queryClient,
}));
vi.mock("@gram/client/react-query/restoreSkillVersion.js", () => ({
  useRestoreSkillVersionMutation: () => testState.restore,
}));
vi.mock("./invalidate-skill-queries", () => ({
  invalidateSkillQueries: testState.invalidate,
}));
vi.mock("sonner", () => ({
  toast: { success: testState.toastSuccess },
}));

const version = {
  id: "version_old",
  canonicalSha256: "abcdef1234567890",
} as SkillVersion;

function RestoreHarness(): JSX.Element {
  const [target, setTarget] = useState<SkillVersion | null>(version);
  return (
    <>
      <button onClick={() => setTarget(version)}>Reopen restore</button>
      <RestoreSkillVersionDialog
        skillId="skill_a"
        version={target}
        direction="backward"
        onClose={() => setTarget(null)}
      />
    </>
  );
}

beforeEach(() => {
  testState.restore.isPending = false;
  testState.restore.mutateAsync.mockReset().mockResolvedValue({});
  testState.invalidate.mockReset().mockResolvedValue(undefined);
  testState.toastSuccess.mockReset();
});

afterEach(cleanup);

describe("RestoreSkillVersionDialog", () => {
  it("confirms the exact restore request and invalidates all skill surfaces", async () => {
    const onClose = vi.fn<() => void>();
    render(
      <RestoreSkillVersionDialog
        skillId="skill_a"
        version={version}
        direction="backward"
        onClose={onClose}
      />,
    );
    expect(
      screen.getByText(
        /Explicit distribution pins for plugins and assistants stay unchanged/,
      ),
    ).toBeTruthy();
    expect(screen.getByText("Roll back to this skill version?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Roll back" }));

    await waitFor(() =>
      expect(testState.restore.mutateAsync).toHaveBeenCalledWith({
        request: {
          restoreSkillVersionRequestBody: {
            id: "skill_a",
            versionId: "version_old",
          },
        },
      }),
    );
    expect(testState.invalidate).toHaveBeenCalledWith(testState.queryClient);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("keeps the dialog open, blocks retry, and resets uncertainty on reopen", async () => {
    testState.restore.mutateAsync.mockRejectedValue(
      new Error("restore failed"),
    );
    render(<RestoreHarness />);
    fireEvent.click(screen.getByRole("button", { name: "Roll back" }));
    expect(await screen.findByText(/restore failed/)).toBeTruthy();
    expect(screen.getByText(/current version may be unknown/)).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Roll back" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Roll back" }));
    expect(testState.restore.mutateAsync).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Reopen restore" }));
    expect(screen.queryByText(/restore failed/)).toBeNull();
    expect(
      screen
        .getByRole("button", { name: "Roll back" })
        .hasAttribute("disabled"),
    ).toBe(false);
  });

  it("disables restore controls until cache reconciliation finishes", async () => {
    const onClose = vi.fn<() => void>();
    let finishInvalidation!: () => void;
    testState.invalidate.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finishInvalidation = resolve;
      }),
    );
    render(
      <RestoreSkillVersionDialog
        skillId="skill_a"
        version={version}
        direction="forward"
        onClose={onClose}
      />,
    );
    expect(screen.getByText("Promote to this skill version?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Promote" }));
    await waitFor(() => expect(testState.invalidate).toHaveBeenCalled());
    expect(
      screen
        .getByRole("button", { name: "Promote..." })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Cancel" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(onClose).not.toHaveBeenCalled();

    await act(async () => finishInvalidation());
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(testState.toastSuccess).toHaveBeenCalledOnce();
  });

  it("does not report a confirmed restore as failed when invalidation fails", async () => {
    const onClose = vi.fn<() => void>();
    testState.invalidate.mockRejectedValue(new Error("refresh failed"));
    render(
      <RestoreSkillVersionDialog
        skillId="skill_a"
        version={version}
        direction="backward"
        onClose={onClose}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Roll back" }));

    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(testState.toastSuccess).toHaveBeenCalledOnce();
    expect(screen.queryByText("Restore failed")).toBeNull();
  });
});
