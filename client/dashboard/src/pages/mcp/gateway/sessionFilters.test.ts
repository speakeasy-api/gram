import { Operator as Op } from "@gram/client/models/components/logfilter";
import { describe, expect, it } from "vitest";
import { parseFilters, serializeFilters } from "@/pages/logs/log-filter-url";
import {
  decodeValues,
  encodeValues,
  filtersToQuery,
  formatSessionFilterValue,
  ownLabel,
  IDENTITY_TYPE_LABELS,
  sessionFilterLimitProblem,
  withinFilterLimits,
  includedValues,
  parseLastUsed,
  parseSessionFilterExpression,
  sessionFilterProblem,
  withIncludedValues,
} from "./sessionFilters";

const chip = (path: string, op: Op, value: string) => ({
  id: `${path}-${op}-${value}`,
  path,
  op,
  value,
});

describe("session filter chips", () => {
  it("round-trips identities containing commas and spaces through the URL", () => {
    const identities = [
      "workload:0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f:repo:a,b/app",
      "workload:0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f: padded ",
    ];
    const chips = withIncludedValues([], "identity", identities);
    const restored = parseFilters(serializeFilters(chips));
    expect(includedValues(restored, "identity")).toEqual(identities);
    expect(filtersToQuery(restored).identities).toEqual(identities);
  });

  it("splits typed lists on commas and trims the separators", () => {
    expect(decodeValues("cimd, dcr ,none")).toEqual(["cimd", "dcr", "none"]);
    expect(decodeValues(encodeValues([" a ", "b"]))).toEqual([" a ", "b"]);
  });

  it("maps chips to include and exclude lists, OR within a field", () => {
    const query = filtersToQuery([
      chip("mcp_server", Op.Eq, "s1"),
      chip("mcp_server", Op.In, encodeValues(["s2", "s3"])),
      chip("client", Op.NotEq, "dashboard"),
      chip("client", Op.NotEq, "none"),
      chip("identity_type", Op.Eq, "agent"),
      chip("client_registration", Op.In, "cimd,dcr"),
      chip("provider", Op.Eq, "none"),
      chip("provider", Op.NotEq, "p2"),
    ]);
    expect(query).toEqual({
      servers: ["s1", "s2", "s3"],
      excludeClients: ["dashboard", "none"],
      identityTypes: ["agent"],
      clientRegistrations: ["cimd", "dcr"],
      providers: ["none"],
      excludeProviders: ["p2"],
    });
  });

  it("reads last used comparisons", () => {
    expect(parseLastUsed("< 1h")).toEqual({ within: 3600 });
    expect(parseLastUsed(">7d")).toEqual({ olderThan: 604800 });
    expect(parseLastUsed("<30m")).toEqual({ within: 1800 });
    expect(parseLastUsed("never")).toEqual({ never: true });
    expect(parseLastUsed("soon")).toBeNull();
    expect(parseLastUsed("< 0h")).toBeNull();
    expect(parseLastUsed("> 3650d")).toEqual({ olderThan: 315360000 });
    expect(parseLastUsed("> 3651d")).toBeNull();
    expect(parseLastUsed(`< ${"9".repeat(400)}d`)).toBeNull();
    expect(
      filtersToQuery([
        chip("last_used", Op.Eq, "> 7d"),
        chip("last_used", Op.Eq, "never"),
      ]),
    ).toEqual({ lastUsedOlderThan: 604800, lastUsedNever: true });
  });

  it("replaces a field's selection with one chip and keeps the rest", () => {
    const start = [
      chip("client", Op.Eq, "c1"),
      chip("client", Op.NotEq, "none"),
      chip("mcp_server", Op.Eq, "s1"),
    ];
    const both = withIncludedValues(start, "client", ["c1", "c2"]);
    expect(includedValues(both, "client")).toEqual(["c1", "c2"]);
    expect(both.filter((c) => c.path === "client")).toHaveLength(2);
    const none = withIncludedValues(both, "client", []);
    expect(none.map((c) => `${c.path} ${c.op} ${c.value}`)).toEqual([
      "client not_eq none",
      "mcp_server eq s1",
    ]);
  });

  it("applies the tightest last-used bound whatever the chip order", () => {
    const within = [
      chip("last_used", Op.Eq, "< 1h"),
      chip("last_used", Op.Eq, "< 24h"),
    ];
    const older = [
      chip("last_used", Op.Eq, "> 7d"),
      chip("last_used", Op.Eq, "> 1d"),
    ];
    for (const chips of [within, [...within].reverse()]) {
      expect(filtersToQuery(chips)).toEqual({ lastUsedWithin: 3600 });
    }
    for (const chips of [older, [...older].reverse()]) {
      expect(filtersToQuery(chips)).toEqual({ lastUsedOlderThan: 604800 });
    }
  });

  it("reads last-used comparisons typed in full", () => {
    expect(parseSessionFilterExpression("last_used > 7d")).toEqual({
      key: "last_used",
      op: Op.Eq,
      value: "> 7d",
    });
    expect(parseSessionFilterExpression("last_used = never")?.value).toBe(
      "never",
    );
    expect(parseSessionFilterExpression("last_used soon")?.value).toBe("soon");
    expect(parseSessionFilterExpression("last_used")).toBeNull();
    expect(parseSessionFilterExpression("client = dashboard")).toBeNull();
  });

  it("refuses chips that would not narrow the list as shown", () => {
    const uuid = "0192f4c8-1a2b-7c3d-8e4f-5a6b7c8d9e0f";
    expect(sessionFilterProblem("client", Op.Eq, uuid)).toBeNull();
    expect(
      sessionFilterProblem("client", Op.In, encodeValues(["dashboard", uuid])),
    ).toBeNull();
    expect(sessionFilterProblem("identity", Op.Eq, " padded ")).toBeNull();
    expect(sessionFilterProblem("last_used", Op.Eq, "< 1h")).toBeNull();

    expect(sessionFilterProblem("color", Op.Eq, "red")).toBe(
      "Unknown field color",
    );
    expect(sessionFilterProblem("client", Op.Contains, "x")).toBe(
      "Client doesn't support that operator",
    );
    expect(sessionFilterProblem("last_used", Op.NotEq, "< 1h")).not.toBeNull();
    expect(sessionFilterProblem("last_used", Op.Eq, "soon")).not.toBeNull();
    expect(sessionFilterProblem("identity", Op.Eq, "")).not.toBeNull();
    expect(sessionFilterProblem("identity", Op.In, "")).not.toBeNull();
    expect(
      sessionFilterProblem("identity_type", Op.Eq, "robot"),
    ).not.toBeNull();
    expect(sessionFilterProblem("mcp_server", Op.Eq, "linear")).not.toBeNull();
    expect(
      sessionFilterProblem("provider", Op.In, encodeValues(["none", uuid])),
    ).toBeNull();
    expect(sessionFilterProblem("provider", Op.Eq, "linear")).not.toBeNull();
    expect(sessionFilterProblem("provider", Op.Eq, "dashboard")).not.toBeNull();
  });

  it("decodes only in-chip values when showing them", () => {
    const label = (_: string, value: string) => `[${value}]`;
    expect(formatSessionFilterValue("identity", Op.Eq, "a,b%2C", label)).toBe(
      "[a,b%2C]",
    );
    expect(
      formatSessionFilterValue(
        "identity",
        Op.In,
        encodeValues(["a,b", "c%"]),
        label,
      ),
    ).toBe("[a,b], [c%]");
  });

  it("ignores inherited keys in the fixed label maps", () => {
    expect(ownLabel(IDENTITY_TYPE_LABELS, "user")).toBe("Person");
    expect(ownLabel(IDENTITY_TYPE_LABELS, "toString")).toBeUndefined();
    for (const key of [
      "constructor",
      "toString",
      "__proto__",
      "hasOwnProperty",
    ]) {
      expect(sessionFilterProblem("identity_type", Op.Eq, key)).not.toBeNull();
      expect(
        sessionFilterProblem("client_registration", Op.Eq, key),
      ).not.toBeNull();
      expect(sessionFilterProblem("client", Op.Eq, key)).not.toBeNull();
    }
  });

  it("caps each include and exclude list at fifty values", () => {
    const ids = Array.from({ length: 51 }, (_, i) => `user:${i}`);
    const fifty = [chip("identity", Op.In, encodeValues(ids.slice(0, 50)))];
    expect(sessionFilterLimitProblem(fifty)).toBeNull();
    expect(
      sessionFilterLimitProblem([...fifty, chip("identity", Op.Eq, ids[50]!)]),
    ).not.toBeNull();
    const excluded = ids.map((id) => chip("identity", Op.NotEq, id));
    expect(sessionFilterLimitProblem(excluded)).not.toBeNull();
    expect(
      sessionFilterLimitProblem([...fifty, ...excluded.slice(0, 50)]),
    ).toBeNull();

    const kept = chip("client", Op.Eq, "dashboard");
    expect(withinFilterLimits([...excluded, kept])).toEqual([kept]);
    expect(withinFilterLimits([...fifty, kept])).toEqual([...fifty, kept]);
  });
});
