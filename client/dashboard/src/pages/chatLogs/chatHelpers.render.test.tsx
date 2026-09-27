import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { highlightMatches } from "./chatHelpers";

afterEach(cleanup);

describe("highlightMatches with a whole-message mask", () => {
  const text = "password is hunter2";

  it("dots out the entire body while masked", () => {
    const { container } = render(
      <>{highlightMatches(text, [], true, true, true)}</>,
    );
    expect(container.textContent).toBe("•".repeat(text.length));
    expect(screen.queryByText(/hunter2/)).toBeNull();
  });

  it("shows the body, spans marked, once revealed", () => {
    const { container } = render(
      <>{highlightMatches(text, ["hunter2"], false, true, true)}</>,
    );
    expect(container.textContent).toBe(text);
    expect(screen.getByText("hunter2").tagName).toBe("MARK");
  });

  it("leaves span-only rows untouched", () => {
    const { container } = render(
      <>{highlightMatches(text, ["hunter2"], true, true)}</>,
    );
    expect(container.textContent).toBe("password is •••••••");
  });
});
