// @vitest-environment node
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerMigrationServerImpact } from "@gram/client/models/components/usersessionissuermigrationserverimpact.js";
import { describe, expect, it } from "vitest";
import {
  authorizationServerModeLabel,
  changesIssuerUrl,
  consolidationTargets,
  describeMismatch,
  describeServerImpact,
  impactServerName,
  providerLabel,
  settingValue,
  unavailableReason,
} from "./issuerDisplay";

function issuer(id: string, projectId = ""): UserSessionIssuer {
  return { id, slug: id, projectId } as UserSessionIssuer;
}

describe("consolidationTargets", () => {
  const pool = [
    issuer("org-a"),
    issuer("org-b"),
    issuer("project-a", "project-1"),
    issuer("project-b", "project-1"),
    issuer("other-project", "project-2"),
  ];

  it("offers an organization-wide source only other organization-wide ones", () => {
    expect(
      consolidationTargets(issuer("org-a"), pool).map((i) => i.id),
    ).toEqual(["org-b"]);
  });

  it("offers a project's own source organization-wide ones and its project's", () => {
    expect(
      consolidationTargets(issuer("project-a", "project-1"), pool).map(
        (i) => i.id,
      ),
    ).toEqual(["org-a", "org-b", "project-b"]);
  });
});

describe("describeMismatch", () => {
  it("words the authorization server mode", () => {
    expect(
      describeMismatch({
        field: "authorization_server_mode",
        sourceValue: "shared",
        targetValue: "endpoint",
      }),
    ).toEqual({ label: "Mode", from: "Shared", to: "One per MCP server" });
  });

  it("words session length in days when it is whole days", () => {
    expect(
      describeMismatch({
        field: "session_duration_hours",
        sourceValue: "336",
        targetValue: "36",
      }),
    ).toEqual({ label: "Session length", from: "14 days", to: "36 hours" });
  });

  it("says two different identity providers differ", () => {
    expect(
      describeMismatch({
        field: "trusted_remote_session_issuer_id",
        sourceValue: "a",
        targetValue: "b",
      }),
    ).toEqual({
      label: "Trusted identity provider",
      from: "One provider",
      to: "A different one",
    });
  });

  it("says two different identity provider clients differ", () => {
    expect(
      describeMismatch({
        field: "trusted_remote_session_client_id",
        sourceValue: "a",
        targetValue: "b",
      }),
    ).toEqual({
      label: "Trusted identity provider client",
      from: "One client",
      to: "A different one",
    });
  });

  it("keeps two values that read the same as they are", () => {
    expect(
      describeMismatch({
        field: "client_id_metadata_admission_mode",
        sourceValue: "reporting",
        targetValue: "open",
      }),
    ).toEqual({
      label: "Client admission",
      from: "Any client",
      to: "Any client",
    });
  });

  it("keeps fields it does not know by their raw names", () => {
    expect(
      describeMismatch({
        field: "new_field",
        sourceValue: "x",
        targetValue: "",
      }),
    ).toEqual({ label: "new_field", from: "x", to: "None" });
  });
});

describe("display helpers", () => {
  it("labels the mode", () => {
    expect(
      authorizationServerModeLabel({
        authorizationServerMode: "shared",
        issuerUrl: "https://app.example.com/oauth/usi/a",
      }),
    ).toBe("Shared");
    expect(
      authorizationServerModeLabel({ authorizationServerMode: "shared" }),
    ).toBe("Shared, unavailable");
    expect(
      authorizationServerModeLabel({ authorizationServerMode: "endpoint" }),
    ).toBe("One per MCP server");
  });

  it("words why a shared authorization server is unavailable", () => {
    expect(
      unavailableReason({
        authorizationServerUnavailableReason: "host_not_served",
      }),
    ).toBe("This deployment doesn't serve its address");
    expect(
      unavailableReason({
        authorizationServerUnavailableReason: "pinned_issuer_url_invalid",
      }),
    ).toBe("Its fixed issuer URL doesn't belong to it");
    expect(unavailableReason({})).toBe("It isn't available right now");
  });

  it("words a setting on its own", () => {
    expect(settingValue("authn_challenge_mode", "interactive")).toBe(
      "Interactive",
    );
  });

  it("notices an issuer URL change", () => {
    expect(
      changesIssuerUrl([
        { field: "issuer_url", sourceValue: "a", targetValue: "b" },
      ]),
    ).toBe(true);
    expect(changesIssuerUrl([])).toBe(false);
  });
});

describe("describeServerImpact", () => {
  const google = {
    remoteSessionClientId: "google",
    clientId: "google-app",
    providerIssuer: "https://accounts.google.com/",
  };
  const slack = {
    remoteSessionClientId: "slack",
    clientId: "slack-app",
    providerIssuer: "https://slack.com",
  };
  function impact(
    overrides: Partial<UserSessionIssuerMigrationServerImpact>,
  ): UserSessionIssuerMigrationServerImpact {
    return {
      mcpServerId: "server",
      mcpServerName: "",
      mcpServerSlug: "",
      projectId: "project",
      projectName: "Project",
      impactSeverity: "warning",
      reasons: [],
      clientsBefore: [],
      clientsAfter: [],
      pinnedClientIds: [],
      ...overrides,
    };
  }

  it("leads with the reason that blocks the move", () => {
    expect(
      describeServerImpact(
        impact({
          reasons: ["provider_not_in_metadata", "gains_client"],
          clientsAfter: [google],
        }),
      ),
    ).toBe(
      "Would use an OAuth client for accounts.google.com, which its upstream doesn't accept.",
    );
  });

  it("names both providers when they change", () => {
    expect(
      describeServerImpact(
        impact({
          reasons: ["changes_provider", "pinned"],
          clientsBefore: [slack],
          clientsAfter: [slack, google],
        }),
      ),
    ).toBe("Would switch from slack.com to slack.com and accounts.google.com.");
  });

  it("says a pinned server keeps its client", () => {
    expect(describeServerImpact(impact({ reasons: ["pinned"] }))).toBe(
      "Keeps its current OAuth client.",
    );
  });

  it("names a server without a name by its slug, then a plain noun", () => {
    expect(impactServerName(impact({ mcpServerSlug: "slack" }))).toBe("slack");
    expect(impactServerName(impact({}))).toBe("Unnamed MCP server");
  });

  it("labels a provider it can't parse by its raw value", () => {
    expect(providerLabel({ providerIssuer: "not a url" })).toBe("not a url");
    expect(providerLabel({ providerIssuer: "" })).toBe("an unknown provider");
  });
});
