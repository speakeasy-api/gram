import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LiftKillswitchDialog } from "./LiftKillswitchDialog";

afterEach(cleanup);

describe("LiftKillswitchDialog", () => {
  it("states which overlapping Killswitches remain effective before lifting", async () => {
    const onLift = vi.fn().mockResolvedValue(undefined);
    render(
      <LiftKillswitchDialog
        open
        onOpenChange={() => {}}
        overlaps={[
          {
            id: "overlap-1",
            scope: { type: "all_servers" },
            schedule: { start: "now", end: "until_lifted" },
            status: "active",
          },
        ]}
        overlapsTruncated
        serverNames={new Map()}
        previewStatus="ready"
        onRetryPreview={vi.fn()}
        onLift={onLift}
      />,
    );

    expect(screen.getByText("Access may remain blocked")).not.toBeNull();
    expect(screen.getByText(/All MCP servers/)).not.toBeNull();
    expect(
      screen.getByText("Additional overlapping Killswitches are not shown."),
    ).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    expect(onLift).toHaveBeenCalledTimes(1);
  });

  it("blocks lifting while overlap refresh fails and retries it", async () => {
    const onRetryPreview = vi.fn().mockResolvedValue(undefined);
    render(
      <LiftKillswitchDialog
        open
        onOpenChange={() => {}}
        overlaps={[]}
        serverNames={new Map()}
        previewStatus="error"
        previewError="preview unavailable"
        onRetryPreview={onRetryPreview}
        onLift={vi.fn()}
      />,
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Lift killswitch",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetryPreview).toHaveBeenCalledTimes(1);
  });

  it("keeps the dialog open and reuses the replay ID after transport errors", async () => {
    const onLift = vi.fn().mockRejectedValue(new Error("stale version"));
    render(
      <LiftKillswitchDialog
        open
        onOpenChange={() => {}}
        overlaps={[]}
        serverNames={new Map()}
        previewStatus="ready"
        onRetryPreview={vi.fn()}
        onLift={onLift}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    expect(await screen.findByText("stale version")).not.toBeNull();
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    expect(onLift.mock.calls[0]![0]).toBe(onLift.mock.calls[1]![0]);
  });
});
