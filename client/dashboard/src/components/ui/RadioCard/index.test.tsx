import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RadioCard, RadioCardGroup } from ".";

function ControlledGroup({
  initialValue = null,
  onValueChange = vi.fn(),
  orientation = "vertical" as const,
}: {
  initialValue?: string | null;
  onValueChange?: (value: string) => void;
  orientation?: "vertical" | "horizontal";
}): React.JSX.Element {
  const [value, setValue] = useState<string | null>(initialValue);

  return (
    <RadioCardGroup
      aria-label="View mode"
      orientation={orientation}
      value={value}
      onValueChange={(nextValue) => {
        setValue(nextValue);
        onValueChange(nextValue);
      }}
    >
      <RadioCard value="grid" title="Grid view" />
      <RadioCard value="list" title="List view">
        Shows additional details.
      </RadioCard>
    </RadioCardGroup>
  );
}

afterEach(cleanup);

describe("RadioCard", () => {
  it("supports a controlled null initial selection and reports changes", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <ControlledGroup
        onValueChange={(value) => {
          onValueChange(value);
        }}
      />,
    );

    expect(
      screen
        .getByRole("radio", { name: "Grid view" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("radio", { name: "List view" })
        .getAttribute("aria-checked"),
    ).toBe("false");

    await user.click(screen.getByRole("radio", { name: "List view" }));

    expect(onValueChange).toHaveBeenCalledWith("list");
    expect(
      screen
        .getByRole("radio", { name: "List view" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("selects when the non-interactive card surface is clicked", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <ControlledGroup
        onValueChange={(value) => {
          onValueChange(value);
        }}
      />,
    );

    const card = screen
      .getByText("Shows additional details.")
      .closest("[data-slot=radio-card]");
    expect(card).not.toBeNull();
    await user.click(card!);

    expect(onValueChange).toHaveBeenCalledWith("list");
  });

  it("derives accessible names from title-only and children-only cards", () => {
    render(
      <RadioCardGroup aria-label="Naming examples">
        <RadioCard value="title" title="Title only" />
        <RadioCard value="children">Children only</RadioCard>
      </RadioCardGroup>,
    );

    expect(screen.getByRole("radio", { name: "Title only" })).toBeTruthy();
    expect(screen.getByRole("radio", { name: "Children only" })).toBeTruthy();
  });

  it("uses numeric zero as a visible and accessible label", () => {
    render(
      <RadioCardGroup aria-label="Numeric labels">
        <RadioCard value="zero" title={0} />
      </RadioCardGroup>,
    );

    expect(screen.getByRole("radio", { name: "0" })).toBeTruthy();
    expect(screen.getByText("0")).toBeTruthy();
  });

  it("rejects empty or non-rendering labels at runtime", () => {
    const message =
      "RadioCard requires a non-empty title or children to provide an accessible label.";

    expect(() => render(<RadioCard value="empty" title="" />)).toThrow(message);
    // @ts-expect-error Runtime validation protects untyped consumers.
    expect(() => render(<RadioCard value="null" title={null} />)).toThrow(
      message,
    );
    expect(() => {
      // @ts-expect-error Runtime validation protects untyped consumers.
      render(<RadioCard value="boolean">{false}</RadioCard>);
    }).toThrow(message);
  });

  it("prevents disabled cards from being selected", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <RadioCardGroup
        aria-label="Availability"
        onValueChange={(value) => {
          onValueChange(value);
        }}
      >
        <RadioCard value="unavailable" title="Unavailable" disabled />
        <RadioCard value="available" title="Available" />
      </RadioCardGroup>,
    );

    const radio = screen.getByRole("radio", { name: "Unavailable" });
    expect(radio.hasAttribute("disabled")).toBe(true);
    await user.click(radio.closest("[data-slot=radio-card]")!);

    expect(onValueChange).not.toHaveBeenCalled();
    expect(radio.getAttribute("aria-checked")).toBe("false");
  });

  it("prevents group-disabled cards from selecting or running actions", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    const onSelect = vi.fn();
    render(
      <RadioCardGroup
        aria-label="Disabled choices"
        disabled
        onValueChange={(value) => {
          onValueChange(value);
        }}
      >
        <RadioCard
          value="unavailable"
          title="Unavailable"
          onSelect={() => {
            onSelect();
          }}
        />
      </RadioCardGroup>,
    );

    const radio = screen.getByRole("radio", { name: "Unavailable" });
    const card = radio.closest("[data-slot=radio-card]");
    await user.click(card!);

    expect(radio.hasAttribute("disabled")).toBe(true);
    expect(onValueChange).not.toHaveBeenCalled();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("preserves arrow-key radio navigation", async () => {
    const user = userEvent.setup();
    render(<ControlledGroup initialValue="grid" orientation="horizontal" />);

    const grid = screen.getByRole("radio", { name: "Grid view" });
    const list = screen.getByRole("radio", { name: "List view" });
    grid.focus();
    await user.keyboard("{ArrowRight}");

    expect(document.activeElement).toBe(list);

    // happy-dom moves Radix's roving focus without synthesizing the browser
    // radio activation, so confirm selection through the focused control.
    await user.keyboard(" ");
    expect(list.getAttribute("aria-checked")).toBe("true");
  });

  it("runs card actions for explicit activation, not arrow navigation", async () => {
    const user = userEvent.setup();
    const onGridSelect = vi.fn();
    const onListSelect = vi.fn();
    render(
      <RadioCardGroup
        aria-label="Action choices"
        defaultValue="grid"
        orientation="horizontal"
      >
        <RadioCard
          value="grid"
          title="Grid action"
          onSelect={() => {
            onGridSelect();
          }}
        />
        <RadioCard
          value="list"
          title="List action"
          onSelect={() => {
            onListSelect();
          }}
        />
      </RadioCardGroup>,
    );

    const grid = screen.getByRole("radio", { name: "Grid action" });
    const list = screen.getByRole("radio", { name: "List action" });
    grid.focus();
    await user.keyboard("{ArrowRight}");

    expect(onGridSelect).not.toHaveBeenCalled();
    expect(onListSelect).not.toHaveBeenCalled();

    await user.keyboard(" ");
    expect(list).toBe(document.activeElement);
    expect(onListSelect).toHaveBeenCalledOnce();
  });

  it("selects before running a card action with Enter", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    const onSelect = vi.fn();
    render(
      <RadioCardGroup
        aria-label="Enter action"
        onValueChange={(value) => {
          onValueChange(value);
        }}
      >
        <RadioCard
          value="action"
          title="Action"
          onSelect={() => {
            onSelect();
          }}
        />
      </RadioCardGroup>,
    );

    const radio = screen.getByRole("radio", { name: "Action" });
    radio.focus();
    await user.keyboard("{Enter}");

    expect(radio.getAttribute("aria-checked")).toBe("true");
    expect(onValueChange).toHaveBeenCalledWith("action");
    expect(onSelect).toHaveBeenCalledOnce();
    expect(onValueChange.mock.invocationCallOrder[0]).toBeLessThan(
      onSelect.mock.invocationCallOrder[0]!,
    );
  });

  it("runs a card action each time its surface is clicked", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <RadioCardGroup aria-label="Repeat action" defaultValue="action">
        <RadioCard
          value="action"
          title="Action"
          onSelect={() => {
            onSelect();
          }}
        />
      </RadioCardGroup>,
    );

    const card = screen
      .getByRole("radio", { name: "Action" })
      .closest("[data-slot=radio-card]");
    await user.click(card!);
    await user.click(card!);

    expect(onSelect).toHaveBeenCalledTimes(2);
  });

  it("does not select a card when a nested control is activated", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    const onAction = vi.fn();
    render(
      <RadioCardGroup
        aria-label="Actions"
        onValueChange={(value) => {
          onValueChange(value);
        }}
      >
        <RadioCard value="action" title="Action choice">
          <button
            type="button"
            onClick={() => {
              onAction();
            }}
          >
            Run action
          </button>
        </RadioCard>
      </RadioCardGroup>,
    );

    await user.click(screen.getByRole("button", { name: "Run action" }));

    expect(onAction).toHaveBeenCalledOnce();
    expect(onValueChange).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("radio", { name: "Action choice" })
        .getAttribute("aria-checked"),
    ).toBe("false");
  });

  it("does not activate the card when a nested label is clicked", async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    const onSelect = vi.fn();
    render(
      <RadioCardGroup
        aria-label="Actions"
        onValueChange={(value) => {
          onValueChange(value);
        }}
      >
        <RadioCard
          value="action"
          title="Action choice"
          onSelect={() => {
            onSelect();
          }}
        >
          <label htmlFor="nested-input">Nested label</label>
          <input id="nested-input" />
        </RadioCard>
      </RadioCardGroup>,
    );

    await user.click(screen.getByText("Nested label"));

    expect(onSelect).not.toHaveBeenCalled();
    expect(onValueChange).not.toHaveBeenCalled();
  });

  it("runs the card action for a zero-detail activation", () => {
    const onSelect = vi.fn();
    render(
      <RadioCardGroup aria-label="Actions">
        <RadioCard
          value="action"
          title="Action choice"
          onSelect={() => {
            onSelect();
          }}
        />
      </RadioCardGroup>,
    );

    const radio = screen.getByRole("radio", { name: "Action choice" });
    fireEvent.click(radio, { detail: 0 });

    expect(onSelect).toHaveBeenCalledOnce();
  });

  it("forwards the orientation prop to the radio group", () => {
    const { rerender } = render(<ControlledGroup />);
    const group = screen.getByRole("radiogroup", { name: "View mode" });

    expect(group.getAttribute("data-orientation")).toBe("vertical");

    rerender(<ControlledGroup orientation="horizontal" />);
    expect(group.getAttribute("data-orientation")).toBe("horizontal");
  });
});
