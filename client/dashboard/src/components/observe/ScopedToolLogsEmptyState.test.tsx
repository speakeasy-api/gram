import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ScopedToolLogsEmptyState } from "./ScopedToolLogsEmptyState";

afterEach(cleanup);

describe("ScopedToolLogsEmptyState", () => {
  it("says the identity made no calls", () => {
    render(<ScopedToolLogsEmptyState localToolsHidden={false} />);

    expect(
      screen.getByText("No tool calls from this identity in this period."),
    ).toBeTruthy();
  });

  it("keeps that sentence and adds that local tools are hidden", () => {
    const { container } = render(
      <ScopedToolLogsEmptyState localToolsHidden={true} />,
    );

    expect(container.textContent).toBe(
      "No tool calls from this identity in this period. Local tools are hidden.",
    );
  });
});
