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

describe("Sensor minimum classifier confidence", () => {
  it("is off by default and saves null", async () => {
    const user = userEvent.setup();
    const onSave = renderEditor();
    const field = screen.getByLabelText(
      "Minimum classifier confidence",
    ) as HTMLInputElement;
    expect(field.value).toBe("");
    await user.type(screen.getByLabelText("Name"), "Topics");
    await user.click(screen.getByRole("button", { name: "Save sensor" }));
    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({ minConfidence: null }),
    );
  });

  it("edits and clears a saved value", async () => {
    const user = userEvent.setup();
    const onSave = renderEditor({ ...sensor, minConfidence: 0.6 });
    const field = screen.getByLabelText("Minimum classifier confidence");
    expect((field as HTMLInputElement).value).toBe("0.6");
    // Typing "0." into a number input sanitizes to empty, so set the value.
    fireEvent.change(field, { target: { value: "0.75" } });
    // jsdom's step check uses binary floats, so submit the form directly.
    fireEvent.submit(
      screen.getByRole("button", { name: "Save sensor" }).closest("form")!,
    );
    expect(onSave).toHaveBeenLastCalledWith(
      expect.objectContaining({ minConfidence: 0.75 }),
    );
    await user.clear(field);
    await user.click(screen.getByRole("button", { name: "Save sensor" }));
    expect(onSave).toHaveBeenLastCalledWith(
      expect.objectContaining({ minConfidence: null }),
    );
  });

  it.each(["-0.1", "1.5"])("rejects out-of-range value %s", (value) => {
    const onSave = renderEditor(sensor);
    fireEvent.change(screen.getByLabelText("Minimum classifier confidence"), {
      target: { value },
    });
    fireEvent.submit(
      screen.getByRole("button", { name: "Save sensor" }).closest("form")!,
    );
    expect(onSave).not.toHaveBeenCalled();
    expect(
      screen.getByText("Enter a number from 0 to 1, or leave it empty."),
    ).toBeTruthy();
  });
});

describe("Exclusive sensor authoring hint", () => {
  const exclusive: SigintSensor = {
    ...sensor,
    mode: "exclusive",
    signalIds: ["billing"],
  };
  const signal = (id: string, name: string) => ({
    id,
    name,
    slug: id,
    projectId: "project-id",
    createdAt: new Date("2026-10-01T00:00:00Z"),
    updatedAt: new Date("2026-10-01T00:00:00Z"),
  });

  function renderExclusive(signalIds: string[]) {
    render(
      <SensorEditorDialog
        open
        canWrite
        sensor={{ ...exclusive, signalIds }}
        signals={[signal("billing", "Billing"), signal("other", "Other")]}
        catalogLoading={false}
        catalogError={false}
        hasMoreSignals={false}
        loadingMoreSignals={false}
        pending={false}
        error={null}
        onLoadMoreSignals={vi.fn<() => void>()}
        onRetrySignals={vi.fn<() => void>()}
        onOpenChange={vi.fn<(open: boolean) => void>()}
        onSave={vi.fn<(draft: SensorDraft) => void>()}
      />,
    );
  }

  it("suggests an other option until one is selected", () => {
    renderExclusive(["billing"]);
    expect(
      screen.getByText(/Exclusive sensors always pick one signal/),
    ).toBeTruthy();
  });

  it("stays quiet once an other option is selected", () => {
    renderExclusive(["billing", "other"]);
    expect(
      screen.queryByText(/Exclusive sensors always pick one signal/),
    ).toBeNull();
  });
});
