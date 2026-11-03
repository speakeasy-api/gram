import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { PulseMCPServer } from "./hooks";
import { ManualSetupBadge } from "./ManualSetupBadge";

afterEach(cleanup);
const server: PulseMCPServer = {
  registrySpecifier: "com.example/server",
  description: "Example server",
  version: "1.0.0",
  supportsDcr: false,
  isReadOnly: true,
  toolCount: 1,
  meta: {},
};

it("labels missing evidence as unknown rather than manual setup", () => {
  render(
    <TooltipProvider>
      <ManualSetupBadge server={server} />
    </TooltipProvider>,
  );
  expect(screen.getByText("Setup unknown")).toBeTruthy();
  expect(screen.queryByText("Manual Setup")).toBeNull();
});

it("shows manual setup only when confirmed", () => {
  render(
    <TooltipProvider>
      <ManualSetupBadge server={{ ...server, authSetup: "manual" }} />
    </TooltipProvider>,
  );
  expect(screen.getByText("Manual Setup")).toBeTruthy();
});

it.each(["cimd", "dcr", "none"] as const)(
  "does not warn for %s setup",
  (authSetup) => {
    render(
      <TooltipProvider>
        <ManualSetupBadge server={{ ...server, authSetup }} />
      </TooltipProvider>,
    );
    expect(screen.queryByText("Manual Setup")).toBeNull();
    expect(screen.queryByText("Setup unknown")).toBeNull();
  },
);
