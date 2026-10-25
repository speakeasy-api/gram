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
  it("never shows an unknown code as the main message", () => {
    expect(reasonText("something_new")).toBe("Another failure was recorded.");
  });
});
