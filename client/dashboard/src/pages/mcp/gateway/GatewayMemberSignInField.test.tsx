import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayAuthorizationServersField } from "./GatewayMemberSignInField";

const mocks = vi.hoisted(() => ({
  admin: true,
  forbidden: false,
  read: vi.fn(),
  retry: vi.fn(),
  rows: vi.fn(),
  legacyProviders: vi.fn(),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ scope, children }: { scope: string; children: unknown }) =>
    scope === "org:admin" && mocks.admin ? children : null,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { details: { settings: { href: (id: string) => `/servers/${id}` } } },
  }),
}));
vi.mock("./useGatewayMemberRows", () => ({
  useGatewayMemberRows: () => {
    mocks.rows();
    return {
      rows: [
        {
          classification: "proxied",
          server: { id: "configured", name: "Configured member" },
        },
        {
          classification: "proxied",
          server: { id: "incomplete", name: "Incomplete member" },
        },
        {
          classification: "proxied",
          server: { id: "failed", name: "Unavailable member" },
        },
        {
          classification: "hosted",
          server: { id: "hosted", name: "Hosted member" },
        },
      ],
      isLoading: false,
      isError: false,
    };
  },
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueries: () => [
    { data: { associations: [{ remoteSessionIssuerId: "copied-provider" }] } },
  ],
}));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/RemoteIdentityProvidersField",
  () => ({
    RemoteIdentityProvidersField: ({
      members,
      hiddenIssuerIds,
    }: {
      members: unknown;
      hiddenIssuerIds: ReadonlySet<string>;
    }) => {
      mocks.legacyProviders(hiddenIssuerIds);
      return members;
    },
  }),
);
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: ({ mcpServerId }: { mcpServerId: string }) => {
    mocks.read(mcpServerId);
    const denied = mocks.forbidden;
    return {
      data: denied
        ? undefined
        : {
            status:
              mcpServerId === "incomplete" ? "setup_incomplete" : "configured",
          },
      isError: denied || mcpServerId === "failed",
      error: denied
        ? new ServiceError(
            {
              fault: false,
              id: "test-error",
              message: "Forbidden",
              name: "forbidden",
              temporary: false,
              timeout: false,
            },
            {
              response: new Response(null, { status: 403 }),
              request: new Request("https://example.test"),
              body: "",
            },
          )
        : undefined,
      refetch: mocks.retry,
    };
  },
}));

function renderField(showLegacy = false) {
  const associatedIssuers = showLegacy
    ? [
        {
          id: "copied-provider",
          name: "Copied provider",
          issuer: "https://issuer.example.test",
          slug: "copied-provider",
          organizationId: "org",
          projectId: "project",
          clientIdMetadataDocumentSupported: false,
          oidc: true,
          passthrough: false,
          createdAt: new Date(0),
          updatedAt: new Date(0),
        },
      ]
    : [];
  render(
    <MemoryRouter>
      <GatewayAuthorizationServersField
        metaMcpServerId="gateway"
        associatedIssuers={associatedIssuers}
        allowAdditionalProviders
        projectId="project"
        isLoading={false}
        onAdd={() => {}}
        onEdit={() => {}}
        onDelete={() => {}}
      />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.admin = true;
  mocks.forbidden = false;
});

it("reads typed state per proxied member without offering a gateway mutation", () => {
  renderField();
  expect(mocks.rows).toHaveBeenCalledOnce();
  expect(mocks.legacyProviders).toHaveBeenCalledWith(
    new Set(["copied-provider"]),
  );
  expect(mocks.read.mock.calls).toEqual([
    ["configured"],
    ["incomplete"],
    ["failed"],
  ]);
  expect(screen.getByText("Upstream credentials configured")).toBeTruthy();
  expect(screen.getByText("Upstream credential setup incomplete")).toBeTruthy();
  expect(screen.getByText("Upstream sign-in status unavailable")).toBeTruthy();
  expect(screen.queryByText("Hosted member")).toBeNull();
  expect(
    screen.queryByText(/legacy authorization server link is hidden/),
  ).toBeNull();
  expect(screen.queryByRole("button", { name: /link|unlink/i })).toBeNull();
  expect(
    screen
      .getByRole("link", { name: "Repair upstream credentials" })
      .getAttribute("href"),
  ).toBe("/servers/incomplete#upstream-credentials");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(mocks.retry).toHaveBeenCalledOnce();
});

it("does not link to management for non-admin readers", () => {
  mocks.admin = false;
  renderField();
  expect(screen.getByText("Upstream credentials configured")).toBeTruthy();
  expect(screen.queryByRole("link")).toBeNull();
});

it("makes a hidden legacy link visible as a count without adding mutation controls", () => {
  renderField(true);
  expect(
    screen.getByText(/1 legacy authorization server link is hidden/),
  ).toBeTruthy();
  expect(
    screen.queryByRole("button", { name: /link|unlink|remove|edit/i }),
  ).toBeNull();
});

it("explains the organization read requirement without retry or management for an MCP-only viewer", () => {
  mocks.admin = false;
  mocks.forbidden = true;
  renderField();
  expect(screen.getByText("Configured member")).toBeTruthy();
  expect(
    screen.getAllByText(
      "Organization read access is required to view upstream sign-in status.",
    ),
  ).toHaveLength(3);
  expect(screen.queryByText("Upstream credentials configured")).toBeNull();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(screen.queryByRole("link")).toBeNull();
  expect(mocks.retry).not.toHaveBeenCalled();
});
