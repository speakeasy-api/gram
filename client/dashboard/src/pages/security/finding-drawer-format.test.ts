import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  formatCallTime,
  siblingLocationLabel,
  spanLocation,
  withArticle,
} from "./finding-drawer-format";

const base = {
  id: "f",
  policyId: "p",
  policyVersion: 1,
  createdAt: new Date(0),
  source: "gitleaks",
} satisfies RiskResult;

describe("withArticle", () => {
  it.each([
    ["Acme Support Tools", "an Acme Support Tools"],
    ["user prompt", "a user prompt"],
    ["assistant message", "an assistant message"],
    ["GitHub", "a GitHub"],
    ["untrusted tool", "an untrusted tool"],
  ])("%s", (word, expected) => {
    expect(withArticle(word)).toBe(expected);
  });
});

describe("formatCallTime", () => {
  it("omits milliseconds when they are zero", () => {
    expect(formatCallTime(new Date(2026, 0, 1, 9, 5, 7))).toBe("09:05:07");
  });

  it("shows milliseconds when present", () => {
    expect(formatCallTime(new Date(2026, 0, 1, 9, 5, 7, 42))).toBe(
      "09:05:07.042",
    );
  });
});

describe("spanLocation", () => {
  it("joins field and JSON sub-path", () => {
    expect(
      spanLocation({
        ...base,
        spans: [{ match: "", field: "tool.args", path: "payload.sql" }],
      }),
    ).toBe("tool.args.payload.sql");
  });

  it("uses whichever of field or path is reported", () => {
    expect(spanLocation({ ...base, spans: [{ match: "", path: "q" }] })).toBe(
      "q",
    );
    expect(
      spanLocation({ ...base, spans: [{ match: "", field: "content" }] }),
    ).toBe("content");
  });

  it("is undefined without a located span", () => {
    expect(spanLocation({ ...base, spans: [{ match: "" }] })).toBeUndefined();
    expect(spanLocation(base)).toBeUndefined();
  });
});

describe("siblingLocationLabel", () => {
  it("shows the server's byte offsets unconverted", () => {
    expect(
      siblingLocationLabel({
        ...base,
        startPos: 146,
        endPos: 176,
        spans: [{ match: "", path: "query" }],
      }),
    ).toBe("query · bytes 146–176");
  });

  it("drops the location prefix when there is none", () => {
    expect(siblingLocationLabel({ ...base, startPos: 1, endPos: 3 })).toBe(
      "bytes 1–3",
    );
  });

  it("is empty without positions or location", () => {
    expect(siblingLocationLabel(base)).toBe("");
  });
});
