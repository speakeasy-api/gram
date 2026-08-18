import { describe, expect, it } from "vitest";
import {
  PROJECT_GUIDE_OVERVIEW_FROM,
  allTimeOverviewScope,
} from "./allTimeOverviewQuery";

describe("allTimeOverviewScope", () => {
  it("starts at the fixed all-time boundary", () => {
    const scope = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:30.500Z"),
    });
    expect(scope).toEqual({
      organization: "org",
      project: "proj",
      range: {
        from: PROJECT_GUIDE_OVERVIEW_FROM,
        to: "2026-08-17T12:01:00.000Z",
      },
    });
  });

  it("keeps the same key for a minute so the query is not re-issued per render", () => {
    const a = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:01.000Z"),
    });
    const b = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:59.000Z"),
    });
    expect(a).toEqual(b);
  });

  it("rounds a boundary instant up to the next bucket, never backwards", () => {
    const scope = allTimeOverviewScope({
      organization: "org",
      project: "proj",
      now: new Date("2026-08-17T12:00:00.000Z"),
    });
    expect(scope.range).toEqual({
      from: PROJECT_GUIDE_OVERVIEW_FROM,
      to: "2026-08-17T12:01:00.000Z",
    });
  });
});
