import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { StepVerify } from "./StepVerify";

const gatewayURL = "https://app.example.test/agent-mcp/agent_example";

afterEach(cleanup);

describe("verify step", () => {
  it("offers troubleshooting while it waits for the first call", () => {
    render(<StepVerify state="waiting" gatewayURL={gatewayURL} />);
    expect(screen.getByText("If nothing arrives")).toBeTruthy();
  });

  it("drops the troubleshooting once the agent has called", () => {
    render(
      <StepVerify
        state="connected"
        firstCallAt={new Date("2026-01-01T00:00:00Z")}
        gatewayURL={gatewayURL}
      />,
    );
    expect(screen.getByText("Connected")).toBeTruthy();
    expect(screen.queryByText("If nothing arrives")).toBeNull();
  });
});
