import { TooltipProvider } from "@/components/ui/tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  LOG_DATA_RETENTION_MESSAGE,
  LoggingPageHeader,
} from "./LoggingPageHeader";

afterEach(cleanup);

describe("LoggingPageHeader", () => {
  it("shows the log data retention period", async () => {
    render(
      <TooltipProvider>
        <LoggingPageHeader
          title="Tool Logs"
          description="Inspect captured tool calls"
        />
      </TooltipProvider>,
    );

    expect(screen.getByRole("heading", { name: "Tool Logs" })).toBeTruthy();

    fireEvent.focus(
      screen.getByRole("button", { name: "About data retention" }),
    );

    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(LOG_DATA_RETENTION_MESSAGE);
  });
});
