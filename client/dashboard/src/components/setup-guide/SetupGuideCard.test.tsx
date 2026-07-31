import type { MCPSetupGuide } from "@gram/client/models/components/mcpsetupguide.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useGetMCPSetupDocs: vi.fn(),
  openPanel: vi.fn(),
  useIsMobile: vi.fn(),
}));

vi.mock("@gram/client/react-query/getMCPSetupDocs.js", () => ({
  useGetMCPSetupDocs: mocks.useGetMCPSetupDocs,
}));

vi.mock("@/components/side-panel/side-panel-context", () => ({
  useSidePanel: () => ({
    openPanel: mocks.openPanel,
    closePanel: vi.fn(),
    descriptor: null,
  }),
}));

vi.mock("@/hooks/use-mobile", () => ({
  useIsMobile: mocks.useIsMobile,
}));

import { SetupGuideCard } from "./SetupGuideCard";

function guide(overrides: Partial<MCPSetupGuide> = {}): MCPSetupGuide {
  return {
    slug: "google-big-query",
    title: "Google BigQuery",
    summary: "Query and manage BigQuery data.",
    aliases: ["com.pulsemcp.mirror/google-bigquery"],
    remotes: [],
    matchKind: "endpoint",
    externalMarkdown: "# BigQuery\n\nOpen the Google Cloud console.\n",
    speakeasyMarkdown: "# Speakeasy setup\n\nClick Add on the catalog entry.\n",
    ...overrides,
  };
}

afterEach(cleanup);

beforeEach(() => {
  mocks.useGetMCPSetupDocs.mockReset();
  mocks.openPanel.mockReset();
  mocks.useIsMobile.mockReturnValue(false);
});

describe("SetupGuideCard", () => {
  it("renders nothing when no guide is published for the server", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [] } });

    const { container } = render(
      <SetupGuideCard serverUrl="https://mcp.example.com/nothing-here" />,
    );

    expect(container.innerHTML).toBe("");
  });

  it("skips the lookup entirely for a server with no upstream endpoint", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: undefined });

    render(<SetupGuideCard />);

    expect(mocks.useGetMCPSetupDocs.mock.calls[0]?.[2]).toMatchObject({
      enabled: false,
    });
  });

  it("prompts to read the guide when one matched the upstream endpoint", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(<SetupGuideCard serverUrl="https://bigquery.googleapis.com/mcp" />);

    expect(
      screen.getByText("This MCP server may require some additional setup."),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: /Read the guide/ })).toBeTruthy();
  });

  it("opens the side panel with the lookup keys, not the loaded guides", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(<SetupGuideCard serverUrl="https://bigquery.googleapis.com/mcp" />);
    fireEvent.click(screen.getByRole("button", { name: /Read the guide/ }));

    expect(mocks.openPanel).toHaveBeenCalledWith({
      kind: "setup-guide",
      title: "Google BigQuery",
      subtitle: "MCP setup guide",
      iconUrl: undefined,
      docsUrl:
        "https://www.speakeasy.com/docs/ai-control-plane/guides/google-big-query",
      props: {
        registrySpecifier: undefined,
        serverUrl: "https://bigquery.googleapis.com/mcp",
      },
    });
  });

  it("titles the panel generically when the two lookup keys matched different guides", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: { guides: [guide(), guide({ slug: "box", title: "Box" })] },
    });

    render(<SetupGuideCard serverUrl="https://bigquery.googleapis.com/mcp" />);
    fireEvent.click(screen.getByRole("button", { name: /Read the guide/ }));

    expect(mocks.openPanel.mock.calls[0]?.[0]).toMatchObject({
      title: "MCP setup guides",
    });
  });

  it("links to the docs on mobile, where there is no room to split the viewport", () => {
    mocks.useIsMobile.mockReturnValue(true);
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(<SetupGuideCard serverUrl="https://bigquery.googleapis.com/mcp" />);

    expect(screen.queryByRole("button", { name: /Read the guide/ })).toBeNull();
    expect(
      screen.getByRole("link", { name: /Read the guide/ }).getAttribute("href"),
    ).toBe(
      "https://www.speakeasy.com/docs/ai-control-plane/guides/google-big-query",
    );
  });

  it("drops the card on mobile when there is no single docs page to open", () => {
    mocks.useIsMobile.mockReturnValue(true);
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: { guides: [guide(), guide({ slug: "box", title: "Box" })] },
    });

    const { container } = render(
      <SetupGuideCard serverUrl="https://bigquery.googleapis.com/mcp" />,
    );

    expect(container.innerHTML).toBe("");
  });
});
