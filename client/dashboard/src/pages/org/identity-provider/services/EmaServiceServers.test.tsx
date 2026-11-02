import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { EMAServiceServer } from "@gram/client/models/components/emaserviceserver.js";
import { EmaServiceServers } from "./EmaServiceServers";
import type { ServiceRow } from "./useEmaServices";

vi.mock("@/routes", () => ({
  useRoutes: ({ projectSlug }: { projectSlug: string }) => ({
    mcp: {
      details: {
        settings: {
          href: (server: string) =>
            `/example/projects/${projectSlug}/mcp/${server}/settings`,
        },
      },
    },
  }),
}));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/IdentityChainingSection",
  () => ({ IDENTITY_CHAINING_SECTION_ID: "identity-chaining" }),
);

const server = (overrides: Partial<EMAServiceServer>): EMAServiceServer => ({
  id: "0198f5b2-0000-7000-8000-000000000001",
  name: "Tracker",
  slug: "tracker",
  projectId: "0198f5b2-0000-7000-8000-0000000000aa",
  projectSlug: "default",
  projectName: "Default",
  status: "working",
  ...overrides,
});

const row = (overrides: Partial<ServiceRow>): ServiceRow => ({
  key: "https://linear.example.com",
  vendorIssuer: "https://linear.example.com",
  name: "Linear",
  isDefault: true,
  providers: [],
  serverCount: 0,
  readyServerCount: 0,
  servers: [],
  hiddenServerCount: 0,
  ...overrides,
});

function show(service: ServiceRow) {
  render(
    <MemoryRouter>
      <EmaServiceServers row={service} />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("Servers on a service page", () => {
  it("lists each server with its project and status, linking to its settings", () => {
    show(
      row({
        serverCount: 3,
        servers: [
          server({}),
          server({
            id: "0198f5b2-0000-7000-8000-000000000002",
            name: "Wiki",
            slug: "wiki",
            projectSlug: "docs",
            projectName: "Docs",
            status: "not_proven",
          }),
          server({
            id: "0198f5b2-0000-7000-8000-000000000003",
            name: "",
            slug: "broken",
            status: "needs_attention",
          }),
        ],
      }),
    );
    expect(
      screen.getByRole("link", { name: "Tracker" }).getAttribute("href"),
    ).toBe("/example/projects/default/mcp/tracker/settings#identity-chaining");
    expect(
      screen.getByRole("link", { name: "Wiki" }).getAttribute("href"),
    ).toBe("/example/projects/docs/mcp/wiki/settings#identity-chaining");
    expect(screen.getByRole("link", { name: "broken" })).toBeTruthy();
    expect(screen.getByText("Working")).toBeTruthy();
    expect(screen.getByText("On, waiting for a first connection")).toBeTruthy();
    expect(screen.getByText("Needs attention")).toBeTruthy();
    expect(screen.getByText("Docs")).toBeTruthy();
    expect(screen.queryByText(/0198f5b2/)).toBeNull();
    expect(screen.queryByText(/don’t have access/)).toBeNull();
  });

  it("shows when the service last accepted a sign-in on each server", () => {
    show(
      row({
        serverCount: 1,
        servers: [
          server({ lastAcceptedAt: new Date(Date.now() - 2 * 60 * 60 * 1000) }),
        ],
      }),
    );
    expect(screen.getByText("Last worked 2h ago")).toBeTruthy();
  });

  it("counts servers the viewer can't see without naming them", () => {
    show(row({ serverCount: 3, servers: [server({})], hiddenServerCount: 2 }));
    expect(
      screen.getByText("2 more servers you don’t have access to."),
    ).toBeTruthy();
    const section = screen.getByRole("table");
    expect(within(section).getAllByRole("link")).toHaveLength(1);
  });

  it("leads with the first action when no server reaches the service", () => {
    show(row({}));
    expect(screen.getByText("No servers connect to Linear yet")).toBeTruthy();
    expect(
      screen.getByText(
        /Turn on Enterprise Managed Authorization in an MCP server’s settings/,
      ),
    ).toBeTruthy();
  });

  it("says every server is hidden when the viewer can see none", () => {
    show(row({ serverCount: 1, hiddenServerCount: 1 }));
    expect(
      screen.getByText("No servers you can see connect to Linear"),
    ).toBeTruthy();
    expect(
      screen.getByText("1 server you don’t have access to connects to it."),
    ).toBeTruthy();
  });

  it("flags servers the accepted providers leave with nobody to vouch", () => {
    show(
      row({
        isDefault: false,
        providers: [
          {
            key: "speakeasy",
            kind: "speakeasy",
            name: "Speakeasy",
            placeholder: false,
            usable: true,
            unavailable: "",
            unusedBySignIn: false,
          },
        ],
        serverCount: 2,
        servers: [
          server({ signInIssuerId: "okta", signInIssuerName: "Okta" }),
          server({
            id: "0198f5b2-0000-7000-8000-000000000002",
            name: "Wiki",
            slug: "wiki",
            status: "not_proven",
          }),
        ],
      }),
    );
    expect(screen.getByText("Needs attention")).toBeTruthy();
    expect(screen.getByText("Okta isn’t enabled for Linear")).toBeTruthy();
    expect(screen.queryByText("Working")).toBeNull();
    expect(screen.getByText("On, waiting for a first connection")).toBeTruthy();
  });

  it("never flags a server under the default", () => {
    show(
      row({
        serverCount: 1,
        servers: [server({ signInIssuerId: "okta", signInIssuerName: "Okta" })],
      }),
    );
    expect(screen.getByText("Working")).toBeTruthy();
    expect(screen.queryByText(/doesn’t accept/)).toBeNull();
  });
});

it("shows unknown server status neutrally even when the known providers do not cover it", () => {
  show(
    row({
      isDefault: false,
      servers: [
        server({ status: "future_status" as EMAServiceServer["status"] }),
      ],
    }),
  );
  expect(screen.getByText("Unknown")).toBeTruthy();
  expect(screen.queryByText("Working")).toBeNull();
  expect(screen.queryByText("Needs attention")).toBeNull();
});
