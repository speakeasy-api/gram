import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { GatewayClientAdmission } from "./GatewayClientAdmission";

type Query = {
  data?: unknown;
  error?: unknown;
  isError?: boolean;
  refetch?: () => void;
};

const state = vi.hoisted(() => ({
  issuer: {} as Query,
  urls: {} as Query,
  issuerCalls: [] as unknown[][],
  urlCalls: [] as unknown[][],
  scopes: ["project:write"] as string[],
}));
vi.mock("@gram/client/react-query/userSessionIssuer.js", () => ({
  useUserSessionIssuer: (...args: unknown[]) => {
    state.issuerCalls.push(args);
    return state.issuer;
  },
}));
vi.mock("@gram/client/react-query/userSessionIssuerCimdClients.js", () => ({
  useUserSessionIssuerCimdClients: (...args: unknown[]) => {
    state.urlCalls.push(args);
    return state.urls;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string, resource: string) =>
      resource === "project-1" && state.scopes.includes(scope),
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      gateway: {
        settings: {
          href: (id: string) => `/default/mcp/gateways/${id}/settings`,
        },
      },
    },
  }),
}));

const issuerWith = (mode: string): Query => ({
  data: {
    id: "issuer-1",
    projectId: "project-1",
    clientIdMetadataAdmissionMode: mode,
  },
});
// The section asks for 11; the API sets a cursor only on a full page.
const urlsOf = (count: number): Query => ({
  data: {
    result: {
      items: Array.from({ length: count }, (_, i) => ({
        id: `cimd-${i}`,
        clientIdMetadataUri: `https://client-${i}.example.test/metadata.json`,
      })),
      nextCursor: count === 11 ? `cimd-${count - 1}` : undefined,
    },
  },
});

beforeEach(() => {
  state.issuer = issuerWith("open");
  state.urls = urlsOf(0);
  state.issuerCalls = [];
  state.urlCalls = [];
  state.scopes = ["project:write"];
});
afterEach(cleanup);

function renderSection(issuerId: string | null = "issuer-1") {
  return render(
    <MemoryRouter>
      <GatewayClientAdmission gatewayId="gateway-1" issuerId={issuerId} />
    </MemoryRouter>,
  );
}

const CURRENT_ONLY =
  /setting in force now .* doesn't revoke existing connections or change dynamic client registration, and existing connections may have been admitted under an earlier setting/;

it("says no setting applies to a gateway without a sign-in server, and reads nothing", () => {
  state.issuer = issuerWith("presets");
  renderSection(null);
  expect(screen.getByText(/has no sign-in server/)).toBeTruthy();
  expect(state.issuerCalls[0]![2]).toMatchObject({ enabled: false });
  expect(screen.queryByText("Restricted")).toBeNull();
});

it("reads the gateway's own sign-in server", () => {
  renderSection();
  expect(state.issuerCalls[0]![0]).toEqual({ id: "issuer-1" });
  expect(state.issuerCalls[0]![2]).toMatchObject({ enabled: true });
});

it("never reads as open while loading, refused, failed or unrecognized", () => {
  const cases: [Query, RegExp][] = [
    [{}, /^$/],
    [
      {
        error: Object.assign(new Error("no"), { statusCode: 404 }),
        isError: true,
      },
      /isn't visible to you, so the setting is unknown/,
    ],
    [
      {
        error: Object.assign(new Error("no"), { statusCode: 403 }),
        isError: true,
      },
      /isn't visible to you, so the setting is unknown/,
    ],
    [issuerWith("something-new"), /doesn't recognize, so it is unknown/],
    [issuerWith("constructor"), /doesn't recognize, so it is unknown/],
    [issuerWith("toString"), /doesn't recognize, so it is unknown/],
    [issuerWith("__proto__"), /doesn't recognize, so it is unknown/],
  ];
  for (const [issuer, text] of cases) {
    state.issuer = issuer;
    const { container } = renderSection();
    expect(screen.queryByText(/^Open\.$/)).toBeNull();
    expect(screen.queryByText("Restricted")).toBeNull();
    expect(screen.queryByText(CURRENT_ONLY)).toBeNull();
    if (text.source !== "^$") expect(screen.getByText(text)).toBeTruthy();
    else expect(container.querySelector(".skeleton")).toBeTruthy();
    cleanup();
  }
  expect(state.urlCalls).toHaveLength(0);
});

it("offers a retry when the read fails, still unknown", () => {
  const refetch = vi.fn<() => void>();
  state.issuer = { error: new Error("boom"), isError: true, refetch };
  renderSection();
  expect(
    screen.getByText(/Couldn't load the setting, so it is unknown/),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(refetch).toHaveBeenCalled();
});

it("reads open and the older reporting setting alike, unrestricted", () => {
  for (const mode of ["open", "reporting"]) {
    state.issuer = issuerWith(mode);
    renderSection();
    expect(screen.getByText("Any client.")).toBeTruthy();
    expect(
      screen.getByText(/Any app with a valid client ID metadata document/),
    ).toBeTruthy();
    expect(screen.queryByText("Restricted")).toBeNull();
    expect(screen.getByText(CURRENT_ONLY)).toBeTruthy();
    cleanup();
  }
  expect(state.urlCalls).toHaveLength(0);
});

it("marks disabled restricted while dynamic registration still works", () => {
  state.issuer = issuerWith("disabled");
  renderSection();
  expect(screen.getByText("Off.")).toBeTruthy();
  expect(screen.getByText(/Apps can still register dynamically/)).toBeTruthy();
  expect(screen.getAllByText("Restricted")).toHaveLength(1);
  expect(state.urlCalls).toHaveLength(0);
});

it("lists at most 10 custom URLs for presets and says when more are configured", () => {
  state.issuer = issuerWith("presets");
  state.urls = urlsOf(11);
  renderSection();
  expect(screen.getByText("Verified clients.")).toBeTruthy();
  expect(screen.getAllByText("Restricted")).toHaveLength(1);
  expect(state.urlCalls[0]![0]).toEqual({
    userSessionIssuerId: "issuer-1",
    limit: 11,
  });
  expect(screen.getAllByText(/\.example\.test\/metadata\.json$/)).toHaveLength(
    10,
  );
  expect(
    screen.queryByText("https://client-10.example.test/metadata.json"),
  ).toBeNull();
  expect(
    screen.getByText(/More custom document URLs are configured/),
  ).toBeTruthy();
  expect(screen.getByText(CURRENT_ONLY)).toBeTruthy();
});

it("does not claim more URLs when exactly 10 are configured", () => {
  state.issuer = issuerWith("presets");
  state.urls = urlsOf(10);
  renderSection();
  expect(screen.getAllByText(/\.example\.test\/metadata\.json$/)).toHaveLength(
    10,
  );
  expect(screen.queryByText(/More custom document URLs/)).toBeNull();
});

it("says when presets has no custom URLs, and retries a failed URL read", () => {
  state.issuer = issuerWith("presets");
  renderSection();
  expect(
    screen.getByText("No custom document URLs are configured."),
  ).toBeTruthy();
  cleanup();
  const refetch = vi.fn<() => void>();
  state.urls = { error: new Error("boom"), isError: true, refetch };
  renderSection();
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(refetch).toHaveBeenCalled();
  expect(screen.getByText("Verified clients.")).toBeTruthy();
});

it("never keeps an earlier restricted answer once a refetch fails", () => {
  state.issuer = {
    ...issuerWith("presets"),
    error: new Error("boom"),
    isError: true,
    refetch: vi.fn<() => void>(),
  };
  renderSection();
  expect(
    screen.getByText(/Couldn't load the setting, so it is unknown/),
  ).toBeTruthy();
  expect(screen.queryByText("Restricted")).toBeNull();
  expect(screen.queryByText("Verified clients.")).toBeNull();
  expect(screen.queryByText(CURRENT_ONLY)).toBeNull();
  expect(state.urlCalls).toHaveLength(0);
});

it("links to the gateway's own authentication settings, offering a change only where it can be made", () => {
  const href = "/default/mcp/gateways/gateway-1/settings#authentication";
  renderSection();
  expect(
    screen
      .getByRole("link", { name: "Change in gateway settings" })
      .getAttribute("href"),
  ).toBe(href);
  cleanup();
  state.scopes = ["project:read"];
  renderSection();
  expect(
    screen
      .getByRole("link", { name: "View in gateway settings" })
      .getAttribute("href"),
  ).toBe(href);
  cleanup();
  // An organization-owned sign-in server is read-only in gateway settings.
  state.scopes = ["project:write"];
  state.issuer = {
    data: {
      id: "issuer-1",
      projectId: "",
      clientIdMetadataAdmissionMode: "presets",
    },
  };
  renderSection();
  expect(
    screen
      .getByRole("link", { name: "View in gateway settings" })
      .getAttribute("href"),
  ).toBe(href);
  expect(screen.getAllByText("Restricted")).toHaveLength(1);
});
