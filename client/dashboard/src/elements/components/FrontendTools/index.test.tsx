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
    // The assistant onboarding tools are gated on RBAC grants, and `hasScope`
    // returns false until the grants query resolves. So the first render after
    // a hard page load sees a trimmed set and a later render sees the full one.
    // Calling each tool inline ran its hooks inside FrontendTools' own hook
    // list, so that growth threw "Rendered more hooks than during the previous
    // render" and crashed the page.
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
