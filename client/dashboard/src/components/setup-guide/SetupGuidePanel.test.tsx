import type { MCPSetupGuide } from "@gram/client/models/components/mcpsetupguide.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useGetMCPSetupDocs: vi.fn(),
}));

vi.mock("@gram/client/react-query/getMCPSetupDocs.js", () => ({
  useGetMCPSetupDocs: mocks.useGetMCPSetupDocs,
}));

import { SetupGuidePanel } from "./SetupGuidePanel";

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
});

describe("SetupGuidePanel", () => {
  it("refetches from the lookup keys it was opened with", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    render(
      <SetupGuidePanel
        registrySpecifier="com.pulsemcp.mirror/box"
        serverUrl="https://mcp.box.com"
      />,
    );

    // The panel outlives the page that opened it, so it cannot be handed the
    // guides and must look them up again itself.
    expect(mocks.useGetMCPSetupDocs.mock.calls[0]?.[0]).toEqual({
      registrySpecifier: "com.pulsemcp.mirror/box",
      serverUrl: "https://mcp.box.com",
    });
  });

  it("renders nothing when no guide resolves", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [] } });

    const { container } = render(
      <SetupGuidePanel registrySpecifier="com.example/nothing-here" />,
    );

    expect(container.innerHTML).toBe("");
  });

  it("renders both halves, with the frontmatter and duplicate title stripped", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({ data: { guides: [guide()] } });

    const { container } = render(
      <SetupGuidePanel registrySpecifier="com.pulsemcp.mirror/box" />,
    );

    expect(screen.getByText("Set up in Box")).toBeTruthy();
    expect(screen.getByText("Set up in Gram")).toBeTruthy();
    expect(container.textContent).toContain(
      "Sign in to the Box Admin Console.",
    );
    expect(container.textContent).toContain("Click Add on the catalog entry.");
    expect(container.textContent).not.toContain("setup_version");
    // The guide's own H1s duplicate the headings the panel already provides.
    expect(container.querySelector("h1")).toBeNull();
  });

  it("opens links out to the provider in a new tab, but keeps in-guide anchors in place", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({
            externalMarkdown:
              "Open the [Box console](https://app.box.com/) and see [below](#create-app).\n\n## Create the app {#create-app}\n",
          }),
        ],
      },
    });

    render(<SetupGuidePanel registrySpecifier="com.pulsemcp.mirror/box" />);

    const external = screen.getByRole("link", { name: "Box console" });
    expect(external.getAttribute("target")).toBe("_blank");
    expect(external.getAttribute("rel")).toBe("noopener noreferrer");

    const anchor = screen.getByRole("link", { name: "below" });
    expect(anchor.getAttribute("href")).toBe("#box--create-app");
    expect(anchor.hasAttribute("target")).toBe(false);
    expect(screen.getByRole("heading", { name: "Create the app" }).id).toBe(
      "box--create-app",
    );
  });

  it("points a cross-reference between the two halves at the heading in this panel", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({
            externalMarkdown:
              "Next, [add the server](speakeasy.md#add-server) in Gram.\n",
            speakeasyMarkdown:
              "## Add the server {#add-server}\n\nClick Add, then see [the console steps](./external.md).\n",
          }),
        ],
      },
    });

    render(<SetupGuidePanel registrySpecifier="com.pulsemcp.mirror/box" />);

    const crossReference = screen.getByRole("link", { name: "add the server" });
    expect(crossReference.getAttribute("href")).toBe("#box--add-server");
    expect(screen.getByRole("heading", { name: "Add the server" }).id).toBe(
      "box--add-server",
    );

    // Names no heading, so there is nothing in the panel to point at.
    expect(
      screen.queryByRole("link", { name: "the console steps" }),
    ).toBeNull();
    expect(screen.getByText(/the console steps/)).toBeTruthy();
  });

  it("leaves a non-web scheme to the renderer's sanitizer", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({
            externalMarkdown:
              "Click [here](javascript:alert(1)) to continue.\n",
          }),
        ],
      },
    });

    const { container } = render(
      <SetupGuidePanel registrySpecifier="com.pulsemcp.mirror/box" />,
    );

    expect(container.textContent).toContain("Click here to continue.");
    expect(screen.queryByRole("link", { name: "here" })).toBeNull();
  });

  it("leaves the docs link to the panel header when one guide matched", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [guide({ slug: "google-big-query", title: "BigQuery" })],
      },
    });

    render(<SetupGuidePanel registrySpecifier="com.pulsemcp.mirror/box" />);

    expect(
      screen.queryByRole("link", { name: /Open documentation/ }),
    ).toBeNull();
  });

  it("stacks every guide when the two lookup keys disagree", () => {
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({ slug: "asana", title: "Asana", matchKind: "endpoint" }),
          guide(),
        ],
      },
    });

    render(
      <SetupGuidePanel
        registrySpecifier="com.pulsemcp.mirror/box"
        serverUrl="https://mcp.asana.com/sse"
      />,
    );

    expect(screen.getByText("Set up in Asana")).toBeTruthy();
    expect(screen.getByText("Set up in Box")).toBeTruthy();
    // Each guide carries its own docs link, so the ambiguity never has to be
    // resolved down to one.
    expect(
      screen.getAllByRole("link", { name: /Open documentation/ }),
    ).toHaveLength(2);
  });

  it("keeps a stacked guide's anchors inside the guide that authored them", () => {
    // The heading both guides share, authored with the same id in each.
    const shared =
      "## Connect your credentials {#connect-credentials}\n\nPaste the client id.\n";
    mocks.useGetMCPSetupDocs.mockReturnValue({
      data: {
        guides: [
          guide({
            slug: "asana",
            title: "Asana",
            matchKind: "endpoint",
            externalMarkdown: "First, [connect](#connect-credentials).\n",
            speakeasyMarkdown: shared,
          }),
          guide({
            externalMarkdown:
              "First, [connect](speakeasy.md#connect-credentials).\n",
            speakeasyMarkdown: shared,
          }),
        ],
      },
    });

    render(
      <SetupGuidePanel
        registrySpecifier="com.pulsemcp.mirror/box"
        serverUrl="https://mcp.asana.com/sse"
      />,
    );

    const [asanaLink, boxLink] = screen.getAllByRole("link", {
      name: "connect",
    });
    expect(asanaLink?.getAttribute("href")).toBe("#asana--connect-credentials");
    expect(boxLink?.getAttribute("href")).toBe("#box--connect-credentials");

    const headings = screen.getAllByRole("heading", {
      name: "Connect your credentials",
    });
    expect(headings.map((heading) => heading.id)).toEqual([
      "asana--connect-credentials",
      "box--connect-credentials",
    ]);
  });
});
