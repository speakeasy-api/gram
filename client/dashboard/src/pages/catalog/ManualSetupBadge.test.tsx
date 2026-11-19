import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import type { CatalogServer } from "./hooks";
import { ManualSetupBadge } from "./ManualSetupBadge";

afterEach(cleanup);
const server: CatalogServer = {
  registrySpecifier: "com.example/server",
  description: "Example server",
  version: "1.0.0",
  supportsDcr: false,
  isReadOnly: true,
  toolCount: 1,
  meta: {},
};
const github: CatalogServer = {
  ...server,
  registrySpecifier: "com.github/mcp",
  authSetup: "unknown",
  remotes: [
    {
      url: "https://api.githubcopilot.com/mcp/",
      transportType: "streamable-http",
    },
  ],
};

function renderBadge(target: CatalogServer) {
  render(
    <TooltipProvider>
      <ManualSetupBadge server={target} />
    </TooltipProvider>,
  );
}

it("labels missing evidence as unknown rather than manual setup", () => {
  renderBadge(server);
  expect(screen.getByText("Setup unknown")).toBeTruthy();
  expect(screen.queryByText("Manual Setup")).toBeNull();
});

it("shows manual setup only when confirmed", () => {
  renderBadge({ ...server, authSetup: "manual" });
  expect(screen.getByText("Manual Setup")).toBeTruthy();
});

it.each(["cimd", "dcr", "none"] as const)(
  "does not warn for %s setup",
  (authSetup) => {
    renderBadge({ ...server, authSetup });
    expect(screen.queryByText("Manual Setup")).toBeNull();
    expect(screen.queryByText("Setup unknown")).toBeNull();
  },
);

it("shows guided setup for a provider with a setup page", () => {
  renderBadge(github);
  expect(screen.getByText("Guided setup")).toBeTruthy();
  expect(screen.queryByText("Setup unknown")).toBeNull();
});

it("replaces manual setup with guided setup", () => {
  renderBadge({ ...github, authSetup: "manual" });
  expect(screen.getByText("Guided setup")).toBeTruthy();
  expect(screen.queryByText("Manual Setup")).toBeNull();
});

it("does not show guided setup for servers without a setup page", () => {
  renderBadge(server);
  expect(screen.getByText("Setup unknown")).toBeTruthy();
  expect(screen.queryByText("Guided setup")).toBeNull();
});
