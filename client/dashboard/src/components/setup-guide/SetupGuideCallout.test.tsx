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

import { SetupGuideCallout } from "./SetupGuideCallout";

function guide(overrides: Partial<MCPSetupGuide> = {}): MCPSetupGuide {
  return {
    slug: "box",
    title: "Box",
    summary: "Access, search, and manage Box content.",
    aliases: ["com.pulsemcp.mirror/box"],
    remotes: [],
    matchKind: "alias",
    externalMarkdown:
      "---\nsetup_version: 1\n---\n\n# Box\n\nSign in to the Box Admin Console.\n",
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

describe("SetupGuideCallout", () => {
  it("renders nothing when no guide is published for the server", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [] } });

    const { container } = render(
      <SetupGuideCallout registrySpecifier="com.example/nothing-here" />,
    );

    expect(container.innerHTML).toBe("");
  });

  it("renders nothing while the lookup is still in flight", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: undefined });

    const { container } = render(
      <SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />,
    );

    expect(container.innerHTML).toBe("");
  });

  it("names the server in the note when one guide matched", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(<SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />);

    expect(screen.getByText("Setup guide available")).toBeTruthy();
    expect(
      screen.getByText("Box needs some setup before it will work in Gram."),
    ).toBeTruthy();
  });

  it("sends both lookup keys when it has both", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [] } });

    render(
      <SetupGuideCallout
        registrySpecifier="com.pulsemcp.mirror/box"
        serverUrl="https://mcp.box.com"
      />,
    );

    expect(mocks.useGetMCPSetupDocs.mock.calls[0]?.[0]).toEqual({
      registrySpecifier: "com.pulsemcp.mirror/box",
      serverUrl: "https://mcp.box.com",
    });
  });

  it("skips the request entirely when it has no lookup key", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: undefined });

    render(<SetupGuideCallout />);

    expect(mocks.useGetMCPSetupDocs.mock.calls[0]?.[2]).toMatchObject({
      enabled: false,
    });
  });

  it("opens the side panel with the lookup keys, not the loaded guides", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [guide({ slug: "google-big-query", title: "BigQuery" })],
      },
    });

    render(
      <SetupGuideCallout
        registrySpecifier="com.pulsemcp.mirror/box"
        serverUrl="https://mcp.box.com"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Read the guide" }));

    // Serializable keys only: the panel survives navigation away from here.
    expect(mocks.openPanel).toHaveBeenCalledWith({
      kind: "setup-guide",
      title: "BigQuery",
      subtitle: "MCP setup guide",
      iconUrl: undefined,
      docsUrl:
        "https://www.speakeasy.com/docs/ai-control-plane/guides/google-big-query",
      props: {
        registrySpecifier: "com.pulsemcp.mirror/box",
        serverUrl: "https://mcp.box.com",
      },
    });
  });

  it("hands the server's icon to the panel without letting it reach the lookup", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(
      <SetupGuideCallout
        registrySpecifier="com.pulsemcp.mirror/box"
        iconUrl="https://cdn.example.com/box.png"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Read the guide" }));

    expect(mocks.openPanel.mock.calls[0]?.[0]).toMatchObject({
      iconUrl: "https://cdn.example.com/box.png",
      docsUrl: "https://www.speakeasy.com/docs/ai-control-plane/guides/box",
    });
    // A presentation detail has no business splitting the query cache.
    expect(mocks.useGetMCPSetupDocs.mock.calls[0]?.[0]).toEqual({
      registrySpecifier: "com.pulsemcp.mirror/box",
    });
  });

  it("titles the panel generically when the two lookup keys matched different guides", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({ slug: "asana", title: "Asana", matchKind: "endpoint" }),
          guide(),
        ],
      },
    });

    render(<SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />);
    fireEvent.click(screen.getByRole("button", { name: "Read the guide" }));

    expect(mocks.openPanel.mock.calls[0]?.[0]).toMatchObject({
      title: "MCP setup guides",
    });
  });

  it("offers only the docs on mobile, where there is no room to split the viewport", () => {
    mocks.useIsMobile.mockReturnValue(true);
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(<SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />);

    expect(screen.queryByRole("button", { name: "Read the guide" })).toBeNull();
    expect(screen.getByRole("link", { name: /Open the Docs/ })).toBeTruthy();
  });

  it("links the callout straight out to the docs, without opening the panel", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [guide({ slug: "google-big-query", title: "BigQuery" })],
      },
    });

    render(<SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />);

    const docs = screen.getByRole("link", { name: /Open the Docs/ });
    expect(docs.getAttribute("href")).toBe(
      "https://www.speakeasy.com/docs/ai-control-plane/guides/google-big-query",
    );
    expect(docs.getAttribute("target")).toBe("_blank");
    expect(docs.getAttribute("rel")).toBe("noopener noreferrer");
    expect(mocks.openPanel).not.toHaveBeenCalled();
  });

  it("drops the callout's docs link when the two lookup keys matched different guides", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({ slug: "asana", title: "Asana", matchKind: "endpoint" }),
          guide(),
        ],
      },
    });

    render(<SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />);

    // Each guide has its own docs page, so one button cannot serve both.
    expect(screen.queryByRole("link", { name: /Open the Docs/ })).toBeNull();
    expect(screen.getByRole("button", { name: "Read the guide" })).toBeTruthy();
  });

  it("renders nothing when neither way of reading the guide is open", () => {
    mocks.useIsMobile.mockReturnValue(true);
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({ slug: "asana", title: "Asana", matchKind: "endpoint" }),
          guide(),
        ],
      },
    });

    // No panel on mobile, and no single docs page for two guides: a banner
    // about work that cannot be started here is just noise.
    const { container } = render(
      <SetupGuideCallout registrySpecifier="com.pulsemcp.mirror/box" />,
    );

    expect(container.innerHTML).toBe("");
  });
});
