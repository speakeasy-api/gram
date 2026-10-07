import type { RemoteMcpServerScopes } from "@gram/client/models/components/remotemcpserverscopes.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ResourceScopePinField } from "./ResourceScopePinField";
import {
  scopePinStatus,
  sharedServerLine,
  unadvertisedPinnedScopes,
  type ResourceScopePin,
} from "./resourceScopePin";

const FLAG_OFF =
  "Not used: pinned scopes are not enabled for your organization.";
const EMPTY_HINT = "Leave empty to use the scopes the MCP server advertises.";

function scopes(
  overrides: Partial<RemoteMcpServerScopes> = {},
): RemoteMcpServerScopes {
  return {
    resourceUrl: "https://mcp.example/mcp",
    pinnedScopes: ["read"],
    advertisedScopesKnown: true,
    advertisedScopes: ["read", "write"],
    challengeScopes: [],
    discoveryEnabled: true,
    sharedServerCount: 0,
    clients: [
      {
        clientId: "client-1",
        scopeSource: "resource_pin",
        requestedScopes: ["read"],
        unadvertisedPinnedScopes: [],
        pinWouldDecide: true,
      },
    ],
    ...overrides,
  };
}

function withSource(
  scopeSource: RemoteMcpServerScopes["clients"][number]["scopeSource"],
  overrides: Partial<RemoteMcpServerScopes> = {},
  unadvertised: string[] = [],
  pinWouldDecide = scopeSource === "resource_pin",
): RemoteMcpServerScopes {
  return scopes({
    clients: [
      {
        clientId: "client-1",
        scopeSource,
        requestedScopes: [],
        unadvertisedPinnedScopes: unadvertised,
        pinWouldDecide,
      },
    ],
    ...overrides,
  });
}

function pin(
  value: string[],
  dirty = false,
  setValue: (values: string[]) => void = () => undefined,
): ResourceScopePin {
  return {
    data: undefined,
    isError: false,
    value,
    setValue,
    dirty,
    save: () => Promise.resolve(false),
    saving: false,
  };
}

afterEach(() => {
  cleanup();
});

describe("scopePinStatus", () => {
  it("offers the advertised fallback for an empty pin only where it applies", () => {
    for (const source of ["cached_resource", "live_resource"] as const) {
      expect(
        scopePinStatus(withSource(source, { pinnedScopes: [] }), "client-1"),
      ).toEqual([EMPTY_HINT]);
    }
    expect(
      scopePinStatus(
        withSource("issuer_catalogue", { pinnedScopes: [] }),
        "client-1",
      ),
    ).toEqual([]);
    expect(
      scopePinStatus(
        withSource("live_resource", {
          pinnedScopes: [],
          discoveryEnabled: false,
        }),
        "client-1",
      ),
    ).toEqual([FLAG_OFF]);
  });

  it("says the pin is used when the server resolves to it", () => {
    expect(scopePinStatus(withSource("resource_pin"), "client-1")).toEqual([
      "Sign-ins request these scopes.",
    ]);
  });

  it("says the connection's own scopes win, pinned or not", () => {
    const line = "Not used: this connection requests its own scopes.";
    expect(scopePinStatus(withSource("client_scope"), "client-1")).toEqual([
      line,
    ]);
    expect(
      scopePinStatus(
        withSource("client_scope", { pinnedScopes: [] }),
        "client-1",
      ),
    ).toEqual([line]);
  });

  it("says the last challenge wins", () => {
    expect(scopePinStatus(withSource("challenge_scope"), "client-1")).toEqual([
      "Not used: the MCP server's last sign-in challenge names the scopes.",
    ]);
  });

  it("puts the rollout flag first, then the source", () => {
    expect(
      scopePinStatus(
        withSource("client_scope", { discoveryEnabled: false }),
        "client-1",
      ),
    ).toEqual([FLAG_OFF, "Not used: this connection requests its own scopes."]);
    expect(
      scopePinStatus(
        withSource("issuer_catalogue", { discoveryEnabled: false }),
        "client-1",
      ),
    ).toEqual([FLAG_OFF]);
    expect(
      scopePinStatus(
        { ...scopes({ discoveryEnabled: false }), pinnedScopes: [] },
        null,
      ),
    ).toEqual([FLAG_OFF]);
  });

  it("stays neutral for a saved pin any other source decides", () => {
    expect(scopePinStatus(withSource("issuer_catalogue"), "client-1")).toEqual([
      "Not used for this connection.",
    ]);
    expect(scopePinStatus(withSource("resource_pin"), "client-2")).toEqual([
      "Not used for this connection.",
    ]);
  });

  it("says nothing about a pin until the connected client is known", () => {
    expect(scopePinStatus(scopes(), null)).toEqual([]);
  });
});

describe("unadvertisedPinnedScopes", () => {
  it("uses only the server's answer for the saved pin", () => {
    expect(
      unadvertisedPinnedScopes(
        withSource("resource_pin", {}, ["admin"]),
        ["read"],
        false,
        "client-1",
      ),
    ).toEqual(["admin"]);
    expect(
      unadvertisedPinnedScopes(
        withSource("resource_pin"),
        ["read", "admin"],
        false,
        "client-1",
      ),
    ).toEqual([]);
  });

  it("checks an edit locally only where the server says the pin would decide", () => {
    const edit = ["read", "admin"];
    expect(
      unadvertisedPinnedScopes(
        withSource("resource_pin"),
        edit,
        true,
        "client-1",
      ),
    ).toEqual(["admin"]);
    expect(
      unadvertisedPinnedScopes(
        withSource("cached_resource", { pinnedScopes: [] }, [], true),
        edit,
        true,
        "client-1",
      ),
    ).toEqual(["admin"]);
    // A client that does not own the resource: no pin, yet the pin would not decide.
    expect(
      unadvertisedPinnedScopes(
        withSource("issuer_catalogue", { pinnedScopes: [] }, [], false),
        edit,
        true,
        "client-1",
      ),
    ).toEqual([]);
    expect(
      unadvertisedPinnedScopes(
        withSource("cached_resource"),
        edit,
        true,
        "client-1",
      ),
    ).toEqual([]);
    expect(
      unadvertisedPinnedScopes(
        withSource("client_scope"),
        edit,
        true,
        "client-1",
      ),
    ).toEqual([]);
    expect(
      unadvertisedPinnedScopes(
        withSource("challenge_scope", { pinnedScopes: [] }),
        edit,
        true,
        "client-1",
      ),
    ).toEqual([]);
    expect(
      unadvertisedPinnedScopes(withSource("resource_pin"), edit, true, null),
    ).toEqual([]);
  });

  it("does not check an edit with the flag off or the list unknown", () => {
    const edit = ["read", "admin"];
    expect(
      unadvertisedPinnedScopes(
        withSource("resource_pin", { discoveryEnabled: false }),
        edit,
        true,
        "client-1",
      ),
    ).toEqual([]);
    expect(
      unadvertisedPinnedScopes(
        withSource("resource_pin", {
          advertisedScopesKnown: false,
          advertisedScopes: undefined,
        }),
        edit,
        true,
        "client-1",
      ),
    ).toEqual([]);
  });
});

describe("sharedServerLine", () => {
  it("pluralises the other servers on the URL", () => {
    expect(sharedServerLine(0)).toBeNull();
    expect(sharedServerLine(1)).toBe(
      "Shared with 1 other MCP server on the same URL.",
    );
    expect(sharedServerLine(3)).toBe(
      "Shared with 3 other MCP servers on the same URL.",
    );
  });
});

describe("ResourceScopePinField", () => {
  it("shows the pinned scopes and the server's status", () => {
    render(
      <ResourceScopePinField
        pin={pin(["read"])}
        scopes={scopes()}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(
      screen.getByRole("combobox", { name: "Pinned scopes" }),
    ).toBeDefined();
    expect(screen.getByText("read")).toBeDefined();
    expect(screen.getByText("Sign-ins request these scopes.")).toBeDefined();
    expect(screen.queryByText(/does not advertise/)).toBeNull();
  });

  it("names the server's unadvertised pinned scopes", () => {
    render(
      <ResourceScopePinField
        pin={pin(["read", "admin"])}
        scopes={scopes({
          pinnedScopes: ["read", "admin"],
          clients: [
            {
              clientId: "client-1",
              scopeSource: "resource_pin",
              requestedScopes: ["read", "admin"],
              unadvertisedPinnedScopes: ["admin"],
              pinWouldDecide: true,
            },
          ],
        })}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(
      screen.getByText(
        "The MCP server does not advertise admin; they are still requested.",
      ),
    ).toBeDefined();
  });

  it("warns about an unsaved scope the MCP server does not advertise", () => {
    render(
      <ResourceScopePinField
        pin={pin(["read", "admin"], true)}
        scopes={scopes()}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(screen.getByText(/does not advertise admin;/)).toBeDefined();
  });

  it("does not warn about an edit the pin would not decide", () => {
    render(
      <ResourceScopePinField
        pin={pin(["admin"], true)}
        scopes={withSource("issuer_catalogue", { pinnedScopes: [] }, [], false)}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(screen.queryByText(/does not advertise/)).toBeNull();
  });

  it("does not warn when the advertised list is unknown", () => {
    render(
      <ResourceScopePinField
        pin={pin(["admin"], true)}
        scopes={scopes({
          advertisedScopesKnown: false,
          advertisedScopes: undefined,
        })}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(screen.queryByText(/does not advertise/)).toBeNull();
  });
  it("is read-only with the flag off and no pin saved", () => {
    render(
      <ResourceScopePinField
        pin={pin([])}
        scopes={scopes({ discoveryEnabled: false, pinnedScopes: [] })}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    const field = screen.getByRole("combobox", { name: "Pinned scopes" });
    expect((field as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("No pinned scopes")).toBeDefined();
    expect(screen.getByText(FLAG_OFF)).toBeDefined();
    expect(screen.queryByText(/enrolled/)).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Clear pinned scopes" }),
    ).toBeNull();
  });

  it("only clears a saved pin with the flag off", () => {
    const setValue = vi.fn<(values: string[]) => void>();
    render(
      <ResourceScopePinField
        pin={pin(["read"], false, setValue)}
        scopes={scopes({ discoveryEnabled: false })}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    const field = screen.getByRole("combobox", { name: "Pinned scopes" });
    expect((field as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Clear pinned scopes" }));
    expect(setValue).toHaveBeenCalledWith([]);
  });

  it("offers no Clear button with the flag on", () => {
    render(
      <ResourceScopePinField
        pin={pin(["read"])}
        scopes={scopes()}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(
      screen.queryByRole("button", { name: "Clear pinned scopes" }),
    ).toBeNull();
  });

  it("offers the advertised scopes as the placeholder with the flag on", () => {
    render(
      <ResourceScopePinField
        pin={pin([])}
        scopes={withSource("cached_resource", { pinnedScopes: [] })}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(screen.getByText("Advertised scopes")).toBeDefined();
    expect(screen.getByText(EMPTY_HINT)).toBeDefined();
  });

  it("names the other servers sharing the URL", () => {
    render(
      <ResourceScopePinField
        pin={pin(["read"])}
        scopes={scopes({ sharedServerCount: 2 })}
        connectedClientId="client-1"
        issuerScopes={[]}
        disabled={false}
      />,
    );

    expect(
      screen.getByText("Shared with 2 other MCP servers on the same URL."),
    ).toBeDefined();
  });
});
