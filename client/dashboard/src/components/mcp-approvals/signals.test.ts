import { describe, expect, it } from "vitest";
import type { EvidenceDocument } from "./evidence";
import { countByTone, evidenceSignals, type SignalTone } from "./signals";

const DAY_MS = 24 * 60 * 60 * 1000;

function remoteDocument(
  overrides: Partial<EvidenceDocument> = {},
): EvidenceDocument {
  return {
    identity: {
      kind: "remote",
      versionPinned: false,
      host: "mcp.example.com",
      registrableDomain: "example.com",
    },
    packageNotPublished: false,
    repositoryNotFound: false,
    capabilities: [],
    gaps: [],
    ...overrides,
  };
}

function packageDocument(
  overrides: Partial<EvidenceDocument> = {},
): EvidenceDocument {
  return {
    identity: {
      kind: "package",
      versionPinned: true,
      registry: "npm",
      packageName: "@acme/mcp",
    },
    packageNotPublished: false,
    repositoryNotFound: false,
    capabilities: [],
    gaps: [],
    ...overrides,
  };
}

function tone(signals: ReturnType<typeof evidenceSignals>, id: string) {
  return signals.find((signal) => signal.id === id)?.tone;
}

function headline(signals: ReturnType<typeof evidenceSignals>, id: string) {
  return signals.find((signal) => signal.id === id)?.headline;
}

function ids(signals: ReturnType<typeof evidenceSignals>) {
  return signals.map((signal) => signal.id);
}

describe("evidenceSignals", () => {
  it("reports nothing when no evidence was gathered", () => {
    // A page with no dossier has no findings to rank — only a gather to
    // report, which the panel says in its own words.
    expect(evidenceSignals(null)).toEqual([]);
  });

  it("orders concerns above everything else and clean checks last", () => {
    const signals = evidenceSignals(
      packageDocument({
        advisories: { knownCount: 2, advisories: [] },
        authority: {
          mode: "oauth",
          scopes: [],
          dynamicRegistration: true,
          demandedSecrets: [],
          optionalSecrets: [],
          unauthenticatedTools: [],
          undeclared: false,
        },
        gaps: ["domain_lookup_failed"],
      }),
    );

    const order = signals.map((signal) => signal.tone);
    const rank: Record<SignalTone, number> = {
      concern: 0,
      watch: 1,
      unknown: 2,
      clean: 3,
    };
    expect(order).toEqual([...order].sort((a, b) => rank[a] - rank[b]));
    expect(order[0]).toBe("concern");
    expect(order.at(-1)).toBe("clean");
  });

  it("does not warn a hosted endpoint about an unpinned version", () => {
    // A remote server has no version to pin, so the caveat is about a property
    // the target does not have. It was on every remote review's header.
    expect(ids(evidenceSignals(remoteDocument()))).not.toContain(
      "version-unpinned",
    );
    expect(
      ids(
        evidenceSignals(
          packageDocument({
            identity: { kind: "package", versionPinned: false },
          }),
        ),
      ),
    ).toContain("version-unpinned");
  });

  it("records advisories as unanswerable for a hosted endpoint", () => {
    // Not silently dropped: the question is real, the database just indexes
    // packages. Ranking it unknown is what lets the panel stop rendering a
    // half-width section that always read empty.
    expect(
      tone(evidenceSignals(remoteDocument()), "advisories-not-applicable"),
    ).toBe("unknown");
  });

  it("treats a checked-and-clean advisory lookup as a check, not a verdict", () => {
    const signals = evidenceSignals(
      packageDocument({ advisories: { knownCount: 0, advisories: [] } }),
    );
    expect(tone(signals, "advisories-clean")).toBe("clean");
    expect(tone(signals, "advisories-found")).toBeUndefined();
  });

  it("does not claim the server declared nothing when the probe failed", () => {
    // "Publishes no authentication metadata" credits the server with an
    // answer. A failed probe is reported as the gap it is, once.
    const signals = evidenceSignals(
      remoteDocument({ gaps: ["authority_probe_failed"] }),
    );
    expect(ids(signals)).not.toContain("authority-undeclared");
    expect(tone(signals, "gap-authority_probe_failed")).toBe("unknown");
  });

  it("reports an undeclared authority when the server answered with nothing", () => {
    expect(
      tone(evidenceSignals(remoteDocument()), "authority-undeclared"),
    ).toBe("unknown");
  });

  it("counts the tools that declare authority, against the whole toolset", () => {
    const signals = evidenceSignals(
      remoteDocument({
        capabilities: [
          {
            tool: "a",
            declared: [],
            schemaImplied: [],
            actsOnBehalf: true,
            unannotated: false,
          },
          {
            tool: "b",
            declared: ["destructive"],
            schemaImplied: [],
            actsOnBehalf: true,
            unannotated: false,
          },
          {
            tool: "c",
            declared: [],
            schemaImplied: [],
            actsOnBehalf: false,
            unannotated: true,
          },
        ],
      }),
    );

    expect(headline(signals, "acts-on-behalf")).toBe(
      "2 of 3 tools declare that they act on your behalf",
    );
    expect(headline(signals, "destructive-tools")).toBe(
      "1 of 3 tools declares destructive effects",
    );
    expect(headline(signals, "unannotated-tools")).toBe(
      "1 of 3 tools declares nothing about what it does",
    );
  });

  it("raises what a tool's input schema accepts, not only what it annotates", () => {
    const signals = evidenceSignals(
      remoteDocument({
        capabilities: [
          {
            tool: "run",
            declared: [],
            schemaImplied: ["arbitrary_command"],
            actsOnBehalf: false,
            unannotated: false,
          },
        ],
      }),
    );

    expect(headline(signals, "schema-arbitrary_command")).toBe(
      "1 tool takes a shell command as a parameter",
    );
  });

  it("names the secret a server demands and ranks it a concern", () => {
    const signals = evidenceSignals(
      remoteDocument({
        authority: {
          mode: "api_key",
          scopes: [],
          dynamicRegistration: false,
          demandedSecrets: [{ name: "ACME_TOKEN", required: true }],
          optionalSecrets: [],
          unauthenticatedTools: [],
          undeclared: false,
        },
      }),
    );

    expect(tone(signals, "demands-secret")).toBe("concern");
    expect(headline(signals, "demands-secret")).toContain("ACME_TOKEN");
    // The mode signal would only restate the same fact in weaker words.
    expect(ids(signals)).not.toContain("api-key-auth");
  });

  it("picks out the scopes whose names grant more than reading", () => {
    const signals = evidenceSignals(
      remoteDocument({
        authority: {
          mode: "oauth",
          scopes: ["read", "write", "tasks:admin", "profile"],
          dynamicRegistration: false,
          demandedSecrets: [],
          optionalSecrets: [],
          unauthenticatedTools: [],
          undeclared: false,
        },
      }),
    );

    expect(headline(signals, "write-scopes")).toBe(
      "The scopes it requests include write, tasks:admin",
    );
  });

  it("says what a denial would cost when the server is already in use", () => {
    const signals = evidenceSignals(
      remoteDocument({
        exposure: { status: "seen", inUse: true, userCount: 3, callCount: 194 },
      }),
    );

    expect(tone(signals, "exposure-in-use")).toBe("watch");
    expect(headline(signals, "exposure-in-use")).toBe(
      "Already in use — denying it changes 3 people's workflow",
    );
  });

  it("separates a young domain from an established one", () => {
    const young = evidenceSignals(
      remoteDocument({
        domain: {
          domain: "example.com",
          unregistered: false,
          registeredAt: new Date(Date.now() - 10 * DAY_MS).toISOString(),
        },
      }),
    );
    expect(tone(young, "domain-young")).toBe("watch");

    const old = evidenceSignals(
      remoteDocument({
        domain: {
          domain: "example.com",
          unregistered: false,
          registeredAt: new Date(Date.now() - 4000 * DAY_MS).toISOString(),
        },
      }),
    );
    expect(tone(old, "domain-established")).toBe("clean");
    expect(ids(old)).not.toContain("domain-young");
  });

  it("carries every gap through as an unknown", () => {
    const signals = evidenceSignals(
      remoteDocument({
        gaps: ["repository_lookup_failed", "catalog_lookup_failed"],
      }),
    );

    expect(tone(signals, "gap-repository_lookup_failed")).toBe("unknown");
    expect(tone(signals, "gap-catalog_lookup_failed")).toBe("unknown");
  });
});

describe("countByTone", () => {
  it("counts each tone, including the ones with nothing in them", () => {
    const counts = countByTone([
      { id: "a", tone: "concern", headline: "" },
      { id: "b", tone: "watch", headline: "" },
      { id: "c", tone: "watch", headline: "" },
    ]);

    expect(counts).toEqual({ concern: 1, watch: 2, unknown: 0, clean: 0 });
  });
});
