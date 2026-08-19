import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";

const catalog = vi.hoisted(() => ({
  current: [] as PulseMCPServer[],
  refetch: vi.fn(),
}));
const workflowOptions = vi.hoisted(() => ({ current: undefined as unknown }));

vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: { servers: catalog.current },
    isPending: false,
    isError: false,
    refetch: catalog.refetch,
  }),
}));

vi.mock("@/pages/catalog/useRemoteMcpInstallWorkflow", () => ({
  useRemoteMcpInstallWorkflow: (options: unknown) => {
    workflowOptions.current = options;
    return { phase: "configure" };
  },
}));

import { ThirdPartyMcpJourney } from "./ThirdPartyMcpJourney";

function server(
  title: string,
  {
    supportsDcr = true,
    remotes = ["streamable-http"],
  }: {
    supportsDcr?: boolean;
    remotes?: ExternalMCPRemote["transportType"][];
  } = {},
): PulseMCPServer {
  return {
    registryId: "catalog",
    registrySpecifier: `example/${title.toLowerCase()}`,
    title,
    description: "Catalog server",
    version: "1.0.0",
    meta: {},
    toolCount: 1,
    isReadOnly: true,
    supportsDcr,
    remotes: remotes.map((transportType, index) => ({
      url: `https://example.test/${title}/${index}`,
      transportType,
    })),
  };
}

afterEach(() => {
  cleanup();
  catalog.current = [];
  catalog.refetch.mockReset();
  workflowOptions.current = undefined;
});

describe("ThirdPartyMcpJourney", () => {
  it("shows only automatic Pulse servers with HTTP remotes in preferred order", () => {
    catalog.current = [
      server("Other"),
      server("Ramp"),
      server("Notion"),
      server("Manual", { supportsDcr: false }),
      server("Vercel"),
      server("Linear"),
      server("SSE only", { remotes: ["sse"] }),
      server("Granola"),
      {
        ...server("Not a Pulse entry"),
        meta: undefined,
      } as unknown as PulseMCPServer,
    ];

    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(
      screen
        .getAllByRole("button")
        .filter((button) => button.textContent !== "Switch journey")
        .map((button) => button.textContent),
    ).toEqual([
      "Linear1 tools",
      "Notion1 tools",
      "Vercel1 tools",
      "Granola1 tools",
      "Ramp1 tools",
      "More automatic servers",
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "More automatic servers" }),
    );
    expect(screen.getByRole("button", { name: /Other/ })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Manual/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /SSE only/ })).toBeNull();
  });

  it("skips selection for a resumed catalog-backed journey", () => {
    render(
      <ThirdPartyMcpJourney
        status="in-progress"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(screen.getByText("Deploy your server")).toBeTruthy();
    expect(screen.queryByText("Pick a server from the catalog")).toBeNull();
  });

  it("shows a retry state when no automatic catalog server is available", () => {
    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    expect(
      screen.getByText("No automatic servers are available right now."),
    ).toBeTruthy();
    expect(screen.queryByText(/deployed/i)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry catalog" }));
    expect(catalog.refetch).toHaveBeenCalledOnce();
  });

  it("advances to deployment and auto-selects every HTTP remote", () => {
    const selected = server("Linear", {
      remotes: ["streamable-http", "streamable-http"],
    });
    catalog.current = [selected];
    render(
      <ThirdPartyMcpJourney
        status="not-started"
        onComplete={vi.fn()}
        onSwitchJourney={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /Linear/ }));

    expect(screen.getByText("Deploy your server")).toBeTruthy();
    expect(workflowOptions.current).toMatchObject({
      servers: [selected],
      autoSelectRemotes: true,
    });
  });
});
