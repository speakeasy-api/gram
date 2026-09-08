import type { AssistantTool } from "@assistant-ui/react";
import { render } from "@testing-library/react";
import { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { FrontendTools } from ".";

// Stand-in for a real frontend tool: `makeAssistantTool` returns a component
// whose `useAssistantTool` calls `useEffect`, so any hook is representative.
const makeTool = (): AssistantTool =>
  (() => {
    useEffect(() => {}, []);
    return null;
  }) as unknown as AssistantTool;

describe("FrontendTools", () => {
  it("survives a tool set that grows between renders", () => {
    const { rerender } = render(<FrontendTools tools={{ a: makeTool() }} />);

    expect(() =>
      rerender(
        <FrontendTools
          tools={{ a: makeTool(), b: makeTool(), c: makeTool() }}
        />,
      ),
    ).not.toThrow();
  });

  it("survives a tool set that shrinks between renders", () => {
    const { rerender } = render(
      <FrontendTools tools={{ a: makeTool(), b: makeTool() }} />,
    );

    expect(() =>
      rerender(<FrontendTools tools={{ a: makeTool() }} />),
    ).not.toThrow();
  });
});
