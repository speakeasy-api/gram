import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import {
  LOG_DATA_RETENTION_MESSAGE,
  LogDataRetentionBanner,
  LoggingPageHeader,
} from "./LoggingPageHeader";

afterEach(cleanup);

describe("LoggingPageHeader", () => {
  it("shows the log data retention period", async () => {
    render(
      <MemoryRouter>
        <TooltipProvider>
          <LoggingPageHeader
            title="Tool Logs"
            description="Inspect captured tool calls"
          />
        </TooltipProvider>
      </MemoryRouter>,
    );

    expect(screen.getByRole("heading", { name: "Tool Logs" })).toBeTruthy();

    fireEvent.focus(
      screen.getByRole("button", { name: "About data retention" }),
    );

    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe(LOG_DATA_RETENTION_MESSAGE);
  });

  it("shows a dismissible retention banner", () => {
    render(<LogDataRetentionBanner />);

    expect(screen.getByText(LOG_DATA_RETENTION_MESSAGE)).toBeTruthy();

    fireEvent.click(screen.getByRole("button"));

    expect(screen.queryByText(LOG_DATA_RETENTION_MESSAGE)).toBeNull();
  });
});
