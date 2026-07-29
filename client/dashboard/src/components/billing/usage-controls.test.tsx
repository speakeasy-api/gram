import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { UsageProgress } from "./usage-controls";

afterEach(cleanup);

describe("UsageProgress", () => {
  it("labels consumed, included, and additional fractional chat credits", () => {
    render(<UsageProgress value={27.5} included={25} unit="chat credits" />);

    expect(screen.getByText("Consumed: 27.5 chat credits")).toBeTruthy();
    expect(screen.getByText("Included: 25 chat credits")).toBeTruthy();
    expect(screen.getByText("Additional: 2.5 chat credits")).toBeTruthy();
  });

  it("labels non-credit quantities with their units", () => {
    render(
      <UsageProgress
        value={250}
        included={1000}
        overageIncrement={1000}
        unit="tool calls"
      />,
    );

    expect(screen.getByText("Consumed: 250 tool calls")).toBeTruthy();
    expect(screen.getByText("Included: 1,000 tool calls")).toBeTruthy();
    expect(screen.queryByText(/Additional:/)).toBeNull();
  });
});
