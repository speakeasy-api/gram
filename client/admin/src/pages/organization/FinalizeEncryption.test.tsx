import { useState } from "react";
import {
  act,
  cleanup,
  fireEvent,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FinalizeEncryption } from "@/pages/organization/FinalizeEncryption";
import { renderWithApp } from "@/test/harness";

const PHRASE = "destroy platform-key copies";

afterEach(() => {
  cleanup();
});

async function open(onSubmit = vi.fn().mockResolvedValue(undefined)) {
  await renderWithApp(
    <FinalizeEncryption
      organizationName="Fixture Org"
      useCase="oauth-client-secrets"
      unreadableValues={3}
      platformCopies={40}
      confirmation={PHRASE}
      disabled={false}
      onSubmit={onSubmit}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Finalize" }));
  const dialog = await screen.findByRole("dialog");
  const submit = () =>
    screen
      .getAllByRole<HTMLButtonElement>("button", { name: "Finalize" })
      .find((button) => dialog.contains(button))!;
  return { dialog, submit, onSubmit };
}

describe("FinalizeEncryption", () => {
  it("states what is destroyed and needs both the phrase and the acknowledgement", async () => {
    const { dialog, submit, onSubmit } = await open();

    expect(dialog.textContent).toContain("40 copies");
    expect(dialog.textContent).toContain("no longer possible");
    expect(dialog.textContent).toContain("3 unreadable values");
    expect(submit().disabled).toBe(true);

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: PHRASE },
    });
    expect(submit().disabled).toBe(true);

    fireEvent.click(screen.getByRole("checkbox"));
    expect(submit().disabled).toBe(false);

    fireEvent.click(submit());
    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith({
        confirmation: PHRASE,
        acknowledgedUnreadableValues: 3,
      }),
    );
  });

  it("keeps the dialog open with the error when finalize is refused", async () => {
    const { submit } = await open(
      vi.fn().mockRejectedValue(new Error("unreadable count changed")),
    );

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: PHRASE },
    });
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(submit());

    expect((await screen.findByRole("alert")).textContent).toContain(
      "unreadable count changed",
    );
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});

describe("FinalizeEncryption acknowledgement", () => {
  it("lapses when the unreadable count changes while the dialog is open", async () => {
    let setCount: (count: number) => void = () => {};
    function Harness() {
      const [count, set] = useState(2);
      setCount = set;
      return (
        <FinalizeEncryption
          organizationName="Fixture Org"
          useCase="oauth-client-secrets"
          unreadableValues={count}
          platformCopies={40}
          confirmation={PHRASE}
          disabled={false}
          onSubmit={vi.fn().mockResolvedValue(undefined)}
        />
      );
    }
    await renderWithApp(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Finalize" }));
    const dialog = await screen.findByRole("dialog");
    const submit = () =>
      screen
        .getAllByRole<HTMLButtonElement>("button", { name: "Finalize" })
        .find((button) => dialog.contains(button))!;

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: PHRASE },
    });
    fireEvent.click(screen.getByRole("checkbox"));
    expect(submit().disabled).toBe(false);

    act(() => setCount(7));
    expect(submit().disabled).toBe(true);
    expect(screen.getByRole("checkbox").getAttribute("data-state")).toBe(
      "unchecked",
    );
    expect(dialog.textContent).toContain("7 unreadable values");
  });
});

describe("FinalizeEncryption blockers", () => {
  it("refuses to submit when blockers appear while the dialog is open", async () => {
    let setDisabled: (disabled: boolean) => void = () => {};
    function Harness() {
      const [disabled, set] = useState(false);
      setDisabled = set;
      return (
        <FinalizeEncryption
          organizationName="Fixture Org"
          useCase="oauth-client-secrets"
          unreadableValues={0}
          platformCopies={1}
          confirmation={PHRASE}
          disabled={disabled}
          onSubmit={vi.fn().mockResolvedValue(undefined)}
        />
      );
    }
    await renderWithApp(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Finalize" }));
    const dialog = await screen.findByRole("dialog");
    const submit = () =>
      screen
        .getAllByRole<HTMLButtonElement>("button", { name: "Finalize" })
        .find((button) => dialog.contains(button))!;

    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: PHRASE },
    });
    fireEvent.click(screen.getByRole("checkbox"));
    expect(submit().disabled).toBe(false);

    act(() => setDisabled(true));
    expect(submit().disabled).toBe(true);
  });
});
