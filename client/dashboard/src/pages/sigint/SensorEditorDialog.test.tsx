import { unrecognized } from "@gram/client/types/unrecognized.js";
import type { SigintSensor } from "@gram/client/models/components/sigintsensor.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SensorEditorDialog, type SensorDraft } from "./SensorEditorDialog";

afterEach(cleanup);

const sensor: SigintSensor = {
  id: "sensor-id",
  projectId: "project-id",
  name: "Support topics",
  slug: "support-topics",
  mode: "multi_label",
  enabled: true,
  matchExpression: 'message.role == "user"',
  inputStrategy: "sliding_window",
  windowSize: 10,
  signalIds: [],
  createdAt: new Date("2026-10-01T00:00:00Z"),
  updatedAt: new Date("2026-10-01T00:00:00Z"),
};

function renderEditor(value: SigintSensor | null = null, canWrite = true) {
  const onSave = vi.fn<(draft: SensorDraft) => void>();
  render(
    <SensorEditorDialog
      open
      canWrite={canWrite}
      sensor={value}
      signals={[]}
      catalogLoading={false}
      catalogError={false}
      hasMoreSignals={false}
      loadingMoreSignals={false}
      pending={false}
      error={null}
      onLoadMoreSignals={vi.fn<() => void>()}
      onRetrySignals={vi.fn<() => void>()}
      onOpenChange={vi.fn<(open: boolean) => void>()}
      onSave={onSave}
    />,
  );
  return onSave;
}

describe("Sensor message context", () => {
  it("defaults to current-message context and offers a five-message window", async () => {
    const user = userEvent.setup();
    const onSave = renderEditor();
    expect(screen.queryByLabelText("Window size")).toBeNull();
    await user.type(screen.getByLabelText("Name"), "Topics");
    await user.click(screen.getByRole("combobox", { name: "Message context" }));
    await user.click(screen.getByRole("option", { name: "Sliding window" }));
    expect(
      (screen.getByLabelText("Window size") as HTMLInputElement).value,
    ).toBe("5");
    await user.click(screen.getByRole("button", { name: "Save sensor" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        inputStrategy: "sliding_window",
        windowSize: 5,
      }),
    );
  });

  it("edits a saved window and preserves classification mode", async () => {
    const user = userEvent.setup();
    const onSave = renderEditor(sensor);
    const size = screen.getByLabelText("Window size");
    expect((size as HTMLInputElement).value).toBe("10");
    await user.clear(size);
    await user.type(size, "2");
    await user.click(screen.getByRole("button", { name: "Save sensor" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        inputStrategy: "sliding_window",
        windowSize: 2,
        mode: "multi_label",
      }),
    );
  });

  it.each(["1", "11", "2.5", ""])("rejects invalid window size %s", (value) => {
    const onSave = renderEditor(sensor);
    fireEvent.change(screen.getByLabelText("Window size"), {
      target: { value },
    });
    const form = screen
      .getByRole("button", { name: "Save sensor" })
      .closest("form")!;
    fireEvent.submit(form);
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.getByText("Enter a whole number from 2 to 10.")).toBeTruthy();
  });

  it("can disable context while retaining the saved window size", async () => {
    const user = userEvent.setup();
    const onSave = renderEditor(sensor);
    await user.click(screen.getByRole("combobox", { name: "Message context" }));
    await user.click(
      screen.getByRole("option", { name: "Current message only" }),
    );
    expect(screen.queryByLabelText("Window size")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Save sensor" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        inputStrategy: "single_message",
        windowSize: 10,
      }),
    );
  });

  it("prevents read-only users from changing context", () => {
    const onSave = renderEditor(sensor, false);
    expect(
      screen
        .getByRole("combobox", { name: "Message context" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(screen.getByLabelText("Window size").hasAttribute("disabled")).toBe(
      true,
    );
    expect(
      screen
        .getByRole("button", { name: "Save sensor" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(onSave).not.toHaveBeenCalled();
  });
});

describe("unsupported sensor configuration", () => {
  it.each([
    { mode: unrecognized("future_mode") },
    { inputStrategy: unrecognized("future_context") },
  ])("does not render an editable form for %j", (unknown) => {
    const onSave = renderEditor({ ...sensor, ...unknown });
    expect(
      screen.getByText(/Refresh the page before editing this sensor/),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Save sensor" })).toBeNull();
    expect(screen.queryByLabelText("Name")).toBeNull();
    expect(onSave).not.toHaveBeenCalled();
  });
});
