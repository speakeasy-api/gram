import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  RepairInferenceKey,
  type RepairInferenceKeyProps,
} from "./RepairInferenceKey";

afterEach(cleanup);
const phrase = "I know what I'm doing";
const causes = [
  {
    cause: "trial_demotion",
    label: "Trial ended",
    description: "The trial lifecycle disabled this key.",
    removable: true,
  },
  {
    cause: "admin_lock",
    label: "Staff lock",
    description: "A staff member locked this key.",
    removable: true,
  },
  {
    cause: "billing_inactive",
    label: "Billing inactive",
    description: "Billing is not eligible.",
    removable: false,
    blockedReason: "An eligible linked subscription is required.",
  },
];
function setup(overrides: Partial<RepairInferenceKeyProps> = {}) {
  const onSubmit = vi.fn(async () => {});
  render(
    <RepairInferenceKey
      keyType="consumer"
      keyName="Consumer inference key"
      causes={causes}
      classified
      disabled
      onSubmit={onSubmit}
      {...overrides}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Repair key locks" }));
  return onSubmit;
}
function confirm(value = phrase, reason = "  BUG-123 stale trial lock  ") {
  fireEvent.change(screen.getByLabelText("Reason or bug ticket"), {
    target: { value: reason },
  });
  fireEvent.change(screen.getByLabelText(`Type ${phrase}`), {
    target: { value },
  });
}
function submit() {
  return screen.getByRole("button", {
    name: "Remove selected locks",
  }) as HTMLButtonElement;
}

it("starts with no removals and previews only explicit selections including staff locks", () => {
  setup();
  for (const checkbox of screen.getAllByRole("checkbox"))
    expect(checkbox.getAttribute("aria-checked")).toBe("false");
  expect(submit().disabled).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  expect(screen.getByRole("status").textContent).toContain("Still disabled");
  expect(screen.getByRole("status").textContent).toContain(
    "Trial ended, Billing inactive",
  );
  expect(screen.getByRole("status").textContent).not.toContain("Staff lock");
});
it("shows the backend billing block reason on keyboard focus", async () => {
  setup();
  expect(
    (
      screen.getByRole("checkbox", {
        name: "Billing inactive",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  act(() =>
    screen
      .getByRole("button", { name: "Why Billing inactive cannot be removed" })
      .focus(),
  );
  expect((await screen.findByRole("tooltip")).textContent).toContain(
    causes[2]!.blockedReason,
  );
});
it.each([
  " I know what I'm doing",
  "I know what I'm doing ",
  "I know what I’m doing",
  "i know what I'm doing",
])("rejects nonexact confirmation %s", (value) => {
  setup();
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  confirm(value);
  expect(submit().disabled).toBe(true);
});
it("requires a nonblank reason and submits trimmed reason, exact confirmation and selected causes only", async () => {
  const onSubmit = setup({ causes: [causes[1]!] });
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  expect(screen.getByRole("status").textContent).toContain(
    "Enabled after repair",
  );
  confirm(phrase, " \n ");
  expect(submit().disabled).toBe(true);
  confirm();
  fireEvent.click(submit());
  await waitFor(() =>
    expect(onSubmit).toHaveBeenCalledWith({
      removeCauses: ["admin_lock"],
      reason: "BUG-123 stale trial lock",
      confirmation: phrase,
    }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.getByRole("status").textContent).toContain("Repair accepted");
});
it("fails closed for unknown and unclassified locks", () => {
  setup({
    classified: false,
    causes: [
      ...causes,
      {
        cause: "future_lock",
        label: "Unknown lock",
        description: "Unknown policy",
        removable: true,
      },
    ],
  });
  expect(screen.getByText(/unclassified disable/i)).toBeTruthy();
  for (const checkbox of screen.getAllByRole("checkbox"))
    expect((checkbox as HTMLButtonElement).disabled).toBe(true);
  expect(submit().disabled).toBe(true);
});
it("keeps unknown causes locked even when classified metadata says removable", () => {
  setup({
    causes: [
      causes[1]!,
      { cause: "future_lock", label: "Unknown lock", removable: true },
    ],
  });
  expect(
    (
      screen.getByRole("checkbox", {
        name: "Unknown lock",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  expect(screen.getByRole("status").textContent).toContain("Unknown lock");
});
it("blocks duplicate submits and closing while pending, preserves errors for retry, restores focus and resets", async () => {
  let reject!: (error: Error) => void;
  const onSubmit = vi
    .fn()
    .mockImplementationOnce(
      () =>
        new Promise<void>((_, fail) => {
          reject = fail;
        }),
    )
    .mockResolvedValue(undefined);
  setup({ onSubmit });
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  confirm();
  fireEvent.click(submit());
  fireEvent.click(screen.getByRole("button", { name: "Removing locks…" }));
  fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
  expect(screen.getByRole("dialog")).toBeTruthy();
  expect(
    (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(onSubmit).toHaveBeenCalledTimes(1);
  await act(async () => reject(new Error("State changed. Review and retry.")));
  expect(screen.getByRole("alert").textContent).toContain("State changed");
  expect(
    (screen.getByLabelText(`Type ${phrase}`) as HTMLInputElement).value,
  ).toBe(phrase);
  fireEvent.click(submit());
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  await waitFor(() =>
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Repair key locks" }),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Repair key locks" }));
  expect(
    (screen.getByLabelText(`Type ${phrase}`) as HTMLInputElement).value,
  ).toBe("");
  expect(
    screen
      .getByRole("checkbox", { name: "Staff lock" })
      .getAttribute("aria-checked"),
  ).toBe("false");
  expect(screen.queryByRole("alert")).toBeNull();
});
it("traps tab focus and restores the trigger on cancel", async () => {
  setup();
  expect(document.activeElement).toBe(
    screen.getByRole("heading", { name: "Repair inference key locks" }),
  );
  const close = screen.getByRole("button", { name: "Close" });
  act(() => close.focus());
  fireEvent.keyDown(close, { key: "Tab" });
  expect(document.activeElement).toBe(
    screen.getByRole("checkbox", { name: "Trial ended" }),
  );
  fireEvent.keyDown(document.activeElement!, { key: "Tab", shiftKey: true });
  expect(document.activeElement).toBe(close);
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() =>
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Repair key locks" }),
    ),
  );
});

it("revalidates selections against refreshed metadata without removing newly blocked locks", () => {
  const onSubmit = vi.fn(async () => {});
  const props = {
    keyType: "consumer",
    keyName: "Consumer inference key",
    classified: true,
    disabled: true,
    onSubmit,
  };
  const { rerender } = render(
    <RepairInferenceKey {...props} causes={[causes[1]!]} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Repair key locks" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  confirm();
  expect(submit().disabled).toBe(false);
  rerender(
    <RepairInferenceKey
      {...props}
      causes={[
        {
          ...causes[1]!,
          removable: false,
          blockedReason: "This lock changed. Reload diagnostics.",
        },
        causes[2]!,
      ]}
    />,
  );
  expect(submit().disabled).toBe(true);
  expect(screen.getByRole("status").textContent).toContain(
    "Staff lock, Billing inactive",
  );
  fireEvent.click(submit());
  expect(onSubmit).not.toHaveBeenCalled();
});

it("fails closed for a disabled key with no classified causes", () => {
  setup({ causes: [] });
  expect(screen.getByText(/unclassified disable/i)).toBeTruthy();
  confirm();
  expect(submit().disabled).toBe(true);
});

it("shows the blocked reason without focusing, hovering or touching the tooltip", () => {
  setup();
  expect(screen.queryByRole("tooltip")).toBeNull();
  const helper = screen.getByText(causes[2]!.blockedReason!);
  expect(helper.hidden).toBe(false);
  expect(helper.closest('[hidden], [aria-hidden="true"]')).toBeNull();
  const checkbox = screen.getByRole("checkbox", {
    name: "Billing inactive",
  }) as HTMLButtonElement;
  expect(checkbox.disabled).toBe(true);
  expect(checkbox.getAttribute("aria-describedby")?.split(" ")).toContain(
    helper.id,
  );
});

it("bounds the trimmed reason by UTF-8 bytes rather than characters", () => {
  setup();
  fireEvent.click(screen.getByRole("checkbox", { name: "Staff lock" }));
  confirm(phrase, "é".repeat(1000));
  expect(submit().disabled).toBe(false);
  confirm(phrase, "é".repeat(1001));
  expect(submit().disabled).toBe(true);
  expect(screen.getByText(/Reason exceeds 2000 UTF-8 bytes/)).toBeTruthy();
});
