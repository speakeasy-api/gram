import { describe, expect, it } from "vitest";
import type { GatewayConnectionHotspot } from "@gram/client/models/components/gatewayconnectionhotspot.js";
import type { GatewayConnectionStage } from "@gram/client/models/components/gatewayconnectionstage.js";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import {
  attemptFilters,
  beforeWindowEnd,
  healthWindow,
  hotspotKey,
  matchingConnectionFilters,
  reasonText,
  stageTiles,
  toAttempt,
} from "./connectionHealth";

const USER = "user:user_example";
const PROVIDER = "6c0d0000-0000-4000-a000-000000000001";
const CLIENT = "6c0d0000-0000-4000-a000-000000000002";

function stage(
  name: GatewayConnectionStage["stage"],
  outcomes: Record<string, number>,
  recovered = 0,
): GatewayConnectionStage {
  const counts = Object.entries(outcomes).map(([outcome, count]) => ({
    outcome,
    count,
  }));
  return {
    stage: name,
    attempts: counts.reduce((sum, c) => sum + c.count, 0),
    outcomes: counts,
    retries: 0,
    recovered,
    buckets: [],
  };
}

describe("healthWindow", () => {
  it("ends at now and spans the chosen window in whole milliseconds", () => {
    const now = new Date("2026-01-08T00:00:00.123Z");
    const { from, to } = healthWindow("7d", now);
    expect(to.getTime()).toBe(now.getTime());
    expect(to.getTime() - from.getTime()).toBe(7 * 24 * 3_600_000);
  });
});

describe("beforeWindowEnd", () => {
  it("drops an event exactly at the end and keeps one a nanosecond before", () => {
    const to = new Date("2026-01-01T00:00:00.005Z");
    const end = BigInt(to.getTime()) * 1_000_000n;
    const events = [
      { timeUnixNano: end.toString() },
      { timeUnixNano: (end - 1n).toString() },
      { timeUnixNano: (end + 1n).toString() },
    ];
    expect(beforeWindowEnd(events, to)).toEqual([
      { timeUnixNano: (end - 1n).toString() },
    ]);
  });
});

describe("stageTiles", () => {
  it("accounts for every inbound outcome, including unknown and enterprise tokens", () => {
    for (const [name, outcomes] of [
      [
        "inbound_authorize",
        { accepted: 2, policy_blocked: 3, failed: 4, unknown: 1 },
      ],
      [
        "inbound_token",
        {
          code_issued: 2,
          refreshed: 3,
          refresh_replayed: 1,
          assertion_issued: 2,
          policy_blocked: 3,
          failed: 4,
          unknown: 1,
        },
      ],
    ] as const) {
      const tiles = stageTiles(stage(name, outcomes));
      expect(tiles.slice(1).reduce((sum, tile) => sum + tile.value, 0)).toBe(
        tiles[0]!.value,
      );
      expect(
        tiles.find((tile) => tile.title === "Outcome not recorded")?.value,
      ).toBe(1);
      expect(
        tiles.find((tile) => tile.title === "Blocked by policy")?.value,
      ).toBe(3);
    }
  });

  it("counts listings against all listings", () => {
    const tiles = stageTiles(
      stage("discovery_listing", { complete: 7, partial: 2, failed: 1 }),
    );
    expect(tiles.map((t) => [t.title, t.value, t.subtext])).toEqual([
      ["Listings", 10, undefined],
      ["Complete", 7, "of 10"],
      ["Partial", 2, "of 10"],
      ["Failed", 1, "of 10"],
    ]);
  });

  it("keeps blocked members in the denominator, apart from failures", () => {
    const tiles = stageTiles(
      stage(
        "discovery_member",
        { listed: 5, failed: 2, credential_blocked: 1, consent_blocked: 2 },
        1,
      ),
    );
    expect(tiles.map((t) => [t.title, t.value, t.subtext])).toEqual([
      ["Member checks", 10, undefined],
      ["Failed", 2, "of 10"],
      ["Needs sign-in or consent", 3, "of 10"],
      ["Listed after an internal retry", 1, "of 10"],
    ]);
  });
});

describe("hotspotKey", () => {
  const hotspot = (keys: [string?, string?, string?]) =>
    ({
      subject: { kind: "user", label: "Person", key: keys[0] },
      provider: { kind: "provider", label: "Provider", key: keys[1] },
      client: { kind: "registered", label: "Client", key: keys[2] },
    }) as GatewayConnectionHotspot;

  it("selects a visible key, including empty values", () => {
    expect(hotspotKey("discovery_listing", hotspot([USER, "", "dcr"]))).toEqual(
      {
        stage: "discovery_listing",
        subject: USER,
        providerId: "",
        client: "dcr",
      },
    );
  });

  it("refuses a key with any hidden part", () => {
    expect(
      hotspotKey("discovery_listing", hotspot([USER, undefined, CLIENT])),
    ).toBeNull();
  });
});

describe("matchingConnectionFilters", () => {
  const key = (providerId: string, client: string, subject = USER) => ({
    stage: "discovery_listing" as const,
    subject,
    providerId,
    client,
  });

  it("maps the caller, a provider ID and a registered client ID", () => {
    const { filters, unmatched } = matchingConnectionFilters(
      key(PROVIDER, CLIENT),
    );
    expect(filters.map((f) => [f.path, f.value])).toEqual([
      ["identity", USER],
      ["provider", PROVIDER],
      ["client", CLIENT],
    ]);
    expect(unmatched).toEqual([]);
  });

  it("names an empty provider and a client kind instead of guessing", () => {
    const { filters, unmatched } = matchingConnectionFilters(key("", "dcr"));
    expect(filters.map((f) => f.path)).toEqual(["identity"]);
    expect(unmatched).toEqual(["provider", "client"]);
  });

  it("maps nothing for an anonymous caller", () => {
    expect(matchingConnectionFilters(key(PROVIDER, CLIENT, ""))).toEqual({
      filters: [],
      unmatched: ["caller"],
    });
  });
});

describe("attemptFilters", () => {
  it("matches every key part exactly, empty values included", () => {
    expect(
      attemptFilters("gateway-1", {
        stage: "discovery_member",
        subject: USER,
        providerId: "",
        client: "",
      }),
    ).toEqual([
      {
        path: "gram.event.urn",
        operator: "eq",
        values: ["urn:telemetry:gram_service:log:mcp_connection_attempt"],
      },
      {
        path: "gram.meta_mcp_server.id",
        operator: "eq",
        values: ["gateway-1"],
      },
      {
        path: "gram.connection_attempt.stage",
        operator: "eq",
        values: ["discovery_member"],
      },
      {
        path: "gram.connection_attempt.subject",
        operator: "eq",
        values: [USER],
      },
      {
        path: "gram.connection_attempt.provider_id",
        operator: "eq",
        values: [""],
      },
      {
        path: "gram.connection_attempt.client_key",
        operator: "eq",
        values: [""],
      },
    ]);
  });
});

describe("toAttempt", () => {
  it("reads nested and flat attempt attributes", () => {
    const base = { id: "a", timeUnixNano: "1767225600000000000" };
    expect(
      toAttempt({
        ...base,
        attributes: {
          gram: {
            connection_attempt: { outcome: "failed", reason: "stale_cursor" },
          },
        },
      } as TelemetryLogRecord),
    ).toMatchObject({ outcome: "failed", reason: "stale_cursor" });
    expect(
      toAttempt({
        ...base,
        attributes: { "gram.connection_attempt.outcome": "complete" },
      } as TelemetryLogRecord),
    ).toMatchObject({ outcome: "complete", reason: "" });
  });
});

describe("reasonText", () => {
  // Every reason the gateway records today (server/internal/mcp).
  const RECORDED = [
    "rate_limited",
    "upstream_unavailable",
    "upstream_rejected",
    "listing_timeout",
    "response_too_large",
    "unreachable",
    "invalid_listing",
    "invalid_response",
    "incomplete_catalog",
    "authorization_server_unavailable",
    "enterprise_authorization_unavailable",
    "listing_budget_exhausted",
    "credential_load_failed",
    "connection_check_failed",
    "unclassified",
    "needs_sign_in",
    "credential_rejected",
    "credential_misconfigured",
    "credential_ambiguous",
    "reauthentication_required",
    "enterprise_authorization_failed",
    "consent_not_approved",
    "consent_auth_changed",
    "consent_config_changed",
    "invalid_request",
    "stale_cursor",
    "member_snapshot_failed",
    "ambiguous_tool_name",
    "inventory_too_large",
    "inventory_error",
    "encoding_failed",
    "canceled",
    "no_member_listed",
    "member_check_failed",
  ];

  // Sign-in and token reasons the gateway records as typed refusals or by its
  // outcome classification (server/internal/mcp/connection_attempts_inbound.go).
  const INBOUND = [
    "access_denied",
    "agent_admission_denied",
    "agent_admission_unavailable",
    "agent_handoff_missing",
    "agent_target_mismatch",
    "ai_tool_blocked",
    "assertion_audience_not_single",
    "assertion_client_presented_secret",
    "assertion_key_source_missing",
    "assertion_subject_invalid",
    "assertion_verifier_unavailable",
    "authorization_expired",
    "canceled",
    "cimd_admission_denied",
    "cimd_admission_disabled",
    "cimd_denied_disabled",
    "cimd_denied_not_listed",
    "cimd_denied_oversized",
    "cimd_denied_unknown_mode",
    "cimd_fetch_failed",
    "cimd_metadata_invalid",
    "client_id_names_endpoint",
    "client_method_unreadable",
    "client_secret_mismatch",
    "code_client_mismatch",
    "code_endpoint_mismatch",
    "code_not_found_or_expired",
    "conflict",
    "device_key_id_mismatch",
    "device_key_lookup_rate_limited",
    "device_key_not_registered",
    "device_key_unavailable",
    "endpoint_not_agent_addressable",
    "federation_unavailable",
    "forbidden",
    "invalid_client",
    "invalid_grant",
    "invalid_request",
    "invalid_scope",
    "invalid_target",
    "issuer_changed_during_rotation",
    "issuer_jwks_uri_invalid",
    "issuer_key_source_unknown",
    "issuer_keys_rate_limited",
    "issuer_lookup_rate_limited",
    "issuer_lookup_unavailable",
    "issuer_untrusted",
    "missing_client_id",
    "missing_token",
    "not_found",
    "oauth_error",
    "pkce_mismatch",
    "public_client_presented_credentials",
    "redirect_uri_mismatch",
    "redirect_uri_not_registered",
    "redirect_validation_failed",
    "refresh_token_client_mismatch",
    "refresh_token_expired",
    "refresh_token_replay_cache_error",
    "refresh_token_replay_context_cancelled",
    "refresh_token_replay_decrypt_error",
    "refresh_token_replay_key_mismatch",
    "refresh_token_replay_payload_invalid",
    "refresh_token_replay_resign_error",
    "refresh_token_replay_session_mismatch",
    "refresh_token_replay_wait_timeout",
    "refresh_token_replay_write_error",
    "refresh_token_resource_mismatch",
    "refresh_token_revoked",
    "refresh_token_unknown_or_already_used",
    "resource_mismatch",
    "resource_not_agent_addressable",
    "response_write_failed",
    "secret_client_presented_assertion",
    "server_error",
    "session_persist_unavailable",
    "sign_in_changed_during_rotation",
    "sign_in_revoked",
    "subject_admission_unavailable",
    "subject_not_admitted",
    "subject_not_authorized",
    "temporarily_unavailable",
    "tool_selection_malformed",
    "tool_selection_resource_mismatch",
    "unauthorized",
    "unauthorized_client",
    "unavailable",
    "unexpected",
    "unknown_client_id",
    "unobserved_exit",
    "unsupported_grant_type",
    "unsupported_response_type",
    "upstream_reconnect_required",
    "workload_assertion_refused",
    "workload_grant_unavailable",
  ];

  it("has plain copy for every typed sign-in and token reason", () => {
    for (const reason of INBOUND) {
      const text = reasonText(reason);
      expect(text, reason).not.toBe("Another failure was recorded.");
      // A one-word code may be a plain word too; a snake_case code never is.
      if (reason.includes("_")) expect(text, reason).not.toContain(reason);
    }
    expect(reasonText("cimd_denied_oversized")).toContain("address");
  });

  it("has plain copy for every recorded reason", () => {
    for (const reason of RECORDED) {
      const text = reasonText(reason);
      expect(text, reason).not.toBe("Another failure was recorded.");
      expect(text, reason).not.toContain(reason);
    }
  });

  it("never shows an unknown code as the main message", () => {
    expect(reasonText("something_new")).toBe("Another failure was recorded.");
  });
});
