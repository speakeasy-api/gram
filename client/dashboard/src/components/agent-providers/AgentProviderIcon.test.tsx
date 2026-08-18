import { render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentProviderIcon, CopilotIcon } from "./AgentProviderIcon";

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

describe("AgentProviderIcon", () => {
  it("renders the LiteLLM logo", () => {
    const { container } = render(<AgentProviderIcon source="litellm" />);

    expect(
      container.querySelector('img[src="/icons/platforms/litellm.png"]'),
    ).toBeTruthy();
  });

  it("renders the mapped provider icon and globe fallback", () => {
    const { container, rerender } = render(
      <AgentProviderIcon source="aws-bedrock" />,
    );

    expect(container.querySelector("svg")).toBeTruthy();

    rerender(<AgentProviderIcon source="unknown-provider" />);
    expect(container.querySelector("svg")).toBeTruthy();
  });
});
