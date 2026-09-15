import { describe, expect, it } from "vitest";
import {
  OBSERVE_FILTER_PARAMS,
  TOOL_NAME_ATTRIBUTE_PATH,
  buildObserveHref,
  carryObserveParams,
} from "./observeDeepLink";

const LOGS = "/org/project/logs";

describe("carryObserveParams", () => {
  it("carries every shared filter param", () => {
    const current = new URLSearchParams();
    for (const key of OBSERVE_FILTER_PARAMS) current.set(key, `v-${key}`);

    const carried = carryObserveParams(current);

    for (const key of OBSERVE_FILTER_PARAMS) {
      expect(carried.get(key)).toBe(`v-${key}`);
    }
  });

  it("leaves page-local search behind", () => {
    const current = new URLSearchParams({
      server: "hosted:payments",
      q: "timeout",
      af: "gram.tool.name:eq:charge",
    });

    const carried = carryObserveParams(current);

    expect(carried.get("server")).toBe("hosted:payments");
    expect(carried.get("q")).toBeNull();
    expect(carried.get("af")).toBeNull();
  });
});

describe("buildObserveHref", () => {
  it("keeps the window and adds the row's server", () => {
    const current = new URLSearchParams({ range: "7d", user: "a@example.com" });

    const href = buildObserveHref(LOGS, current, {
      target: { type: "hosted", id: "payments" },
    });
    const params = new URLSearchParams(href.split("?")[1]);

    expect(params.get("range")).toBe("7d");
    expect(params.get("user")).toBe("a@example.com");
    expect(params.get("server")).toBe("hosted:payments");
  });

  it("appends to an existing server selection rather than replacing it", () => {
    const current = new URLSearchParams({ server: "hosted:payments" });

    const href = buildObserveHref(LOGS, current, {
      target: { type: "gateway", id: "gw-1" },
    });

    expect(new URLSearchParams(href.split("?")[1]).get("server")).toBe(
      "hosted:payments,gateway:gw-1",
    );
  });

  it("does not duplicate a server already selected", () => {
    const current = new URLSearchParams({ server: "hosted:payments" });

    const href = buildObserveHref(LOGS, current, {
      target: { type: "hosted", id: "payments" },
    });

    expect(new URLSearchParams(href.split("?")[1]).get("server")).toBe(
      "hosted:payments",
    );
  });

  it("narrows skill rows by type, since they have no server identity", () => {
    const href = buildObserveHref(LOGS, new URLSearchParams(), {
      targetTypes: ["skill"],
    });

    expect(new URLSearchParams(href.split("?")[1]).get("hookTypes")).toBe(
      "skill",
    );
  });

  it("carries an errors-panel row as a status filter", () => {
    const href = buildObserveHref(LOGS, new URLSearchParams({ range: "1d" }), {
      target: { type: "hosted", id: "payments" },
      statuses: ["error"],
    });
    const params = new URLSearchParams(href.split("?")[1]);

    expect(params.get("status")).toBe("error");
    expect(params.get("server")).toBe("hosted:payments");
    expect(params.get("range")).toBe("1d");
  });

  it("sends a tool name through af, the only encoding the payload supports", () => {
    const href = buildObserveHref(LOGS, new URLSearchParams(), {
      toolName: "charge",
    });

    expect(new URLSearchParams(href.split("?")[1]).get("af")).toBe(
      `${TOOL_NAME_ATTRIBUTE_PATH}:eq:charge`,
    );
  });

  it("merges a tool chip into chips the reader already had", () => {
    const current = new URLSearchParams({ af: "user.region:eq:us-east-1" });

    const href = buildObserveHref(LOGS, current, { toolName: "charge" });
    const af = new URLSearchParams(href.split("?")[1]).get("af") ?? "";

    expect(af).toContain("user.region:eq:us-east-1");
    expect(af).toContain(`${TOOL_NAME_ATTRIBUTE_PATH}:eq:charge`);
  });

  it("carries a client key so client panels can drill in", () => {
    const href = buildObserveHref(LOGS, new URLSearchParams(), {
      clientKey: "claude code",
    });

    expect(new URLSearchParams(href.split("?")[1]).get("client")).toBe(
      "claude code",
    );
  });

  it("returns the bare path when nothing is applied", () => {
    expect(buildObserveHref(LOGS, new URLSearchParams())).toBe(LOGS);
  });
});
