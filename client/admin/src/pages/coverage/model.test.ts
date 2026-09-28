import { describe, expect, it } from "vitest";
import {
  cellFact,
  claimFact,
  notApplicable,
  snapshotSchema,
  summarize,
  unknown,
  type Fact,
  type Method,
  type PlatformSupport,
} from "./model";

const supported: Fact = {
  status: "supported",
  note: "via hooks",
  verify: false,
};
const support = (
  applicability: PlatformSupport["applicability"],
  cells: Record<string, Fact> = {},
): PlatformSupport => ({
  platform: "cli",
  applicability,
  accounts: {
    personal: "supported",
    team: "supported",
    enterprise: "supported",
  },
  note: "",
  cells,
});

describe("cellFact", () => {
  it("states every cell of an applicable entry explicitly", () => {
    expect(
      cellFact(support("applicable", { session: supported }), "session"),
    ).toEqual(supported);
    expect(cellFact(support("applicable"), "session")).toEqual(unknown);
  });
  it("makes a method that does not apply not applicable everywhere", () => {
    expect(cellFact(support("na", { session: supported }), "session")).toEqual(
      notApplicable,
    );
  });
  it("leaves an unassessed entry, or a missing one, unknown", () => {
    expect(
      cellFact(support("unknown", { session: supported }), "session"),
    ).toEqual(unknown);
    expect(cellFact(undefined, "session")).toEqual(unknown);
  });
});

describe("claimFact", () => {
  it("reads the method's own claim", () => {
    const method: Method = {
      id: "hooks",
      name: "Hooks",
      vendor: "Vendor",
      plans: "",
      claims: { session: supported },
      platforms: [],
    };
    expect(claimFact(method, "session")).toEqual(supported);
    expect(claimFact(method, "cost")).toEqual(unknown);
  });
});

describe("summarize", () => {
  const fact = (status: Fact["status"], verify = false): Fact => ({
    status,
    note: "",
    verify,
  });
  it("counts supporting methods and inherits verification only when all need it", () => {
    expect(
      summarize([fact("supported", true), fact("supported"), fact("na")]),
    ).toEqual({ status: "supported", note: "2 methods", verify: false });
    expect(summarize([fact("supported", true)])).toEqual({
      status: "supported",
      note: "1 method",
      verify: true,
    });
  });
  it("falls back to partial, then unknown, then the negatives", () => {
    expect(summarize([fact("partial"), fact("unimplemented")]).status).toBe(
      "partial",
    );
    expect(summarize([fact("unknown"), fact("unimplemented")]).status).toBe(
      "unknown",
    );
    expect(summarize([]).status).toBe("unknown");
    expect(summarize([fact("na"), fact("na")]).status).toBe("na");
    expect(summarize([fact("impossible"), fact("na")]).status).toBe(
      "impossible",
    );
    expect(summarize([fact("impossible"), fact("unimplemented")]).status).toBe(
      "unimplemented",
    );
  });
});

describe("snapshotSchema", () => {
  it("accepts the served shape and rejects an invented status", () => {
    const snapshot = {
      capabilities: [
        { id: "session", name: "Session tracking", group: "Observe" },
      ],
      platforms: [
        {
          id: "cli",
          name: "CLI",
          vendor: "Vendor",
          family: "Agent",
          surface: "CLI",
        },
      ],
      methods: [
        {
          id: "hooks",
          name: "Hooks",
          vendor: "Vendor",
          plans: "Team plans",
          claims: { session: supported },
          platforms: [
            {
              ...support("applicable", { session: supported }),
              os: { mac: "supported", linux: "verify" },
            },
          ],
        },
      ],
      revision: "r1",
    };
    expect(
      snapshotSchema.parse(snapshot).methods[0]?.platforms[0]?.os?.linux,
    ).toBe("verify");
    const bad = structuredClone(snapshot);
    bad.methods[0]!.claims.session = {
      ...supported,
      status: "maybe" as Fact["status"],
    };
    expect(() => snapshotSchema.parse(bad)).toThrow();
  });
});
