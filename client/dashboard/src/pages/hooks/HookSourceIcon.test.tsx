import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopilotIcon } from "./HookSourceIcon";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("CopilotIcon", () => {
  it("renders without invalid SVG property warnings", () => {
    const consoleError = vi
      .spyOn(console, "error")
      .mockImplementation(() => undefined);

    render(<CopilotIcon />);

    expect(consoleError).not.toHaveBeenCalled();
  });
});
