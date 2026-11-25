import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Input } from ".";

afterEach(cleanup);

describe("Input", () => {
  it("does not reference an unrendered generic validation message", () => {
    render(
      <>
        <span id="hint">Hint</span>
        <Input aria-describedby="hint" value="invalid" validate={() => false} />
      </>,
    );

    expect(screen.getByRole("textbox").getAttribute("aria-describedby")).toBe(
      "hint",
    );
  });
});
