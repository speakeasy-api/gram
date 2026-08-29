import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LiftKillswitchDialog } from "./LiftKillswitchDialog";

afterEach(cleanup);

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

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

  it("consumes rejected overlap retry promises", async () => {
    const onRetryPreview = vi
      .fn()
      .mockRejectedValue(new Error("retry still unavailable"));
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

    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetryPreview).toHaveBeenCalledTimes(1);
    expect(screen.getByText("preview unavailable")).not.toBeNull();
  });

  it("cannot be dismissed while the lift mutation is pending", async () => {
    const pending = deferred();
    const onOpenChange = vi.fn();
    render(
      <LiftKillswitchDialog
        open
        onOpenChange={(open) => {
          onOpenChange(open);
        }}
        overlaps={[]}
        serverNames={new Map()}
        previewStatus="ready"
        onRetryPreview={vi.fn()}
        onLift={() => pending.promise}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Lift killswitch" }),
    );
    await waitFor(() =>
      expect(
        (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
          .disabled,
      ).toBe(true),
    );
    await userEvent.click(screen.getByRole("button", { name: "Close" }));
    await userEvent.keyboard("{Escape}");
    fireEvent.pointerDown(document.body);
    expect(onOpenChange).not.toHaveBeenCalled();

    pending.resolve();
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
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
