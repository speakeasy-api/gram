import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CopilotIcon, HookSourceIcon } from "./HookSourceIcon";

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

describe("HookSourceIcon", () => {
  it("renders the LiteLLM logo", () => {
    const { container } = render(<HookSourceIcon source="litellm" />);

    expect(
      container.querySelector('img[src="/icons/platforms/litellm.png"]'),
    ).toBeTruthy();
  });
});
