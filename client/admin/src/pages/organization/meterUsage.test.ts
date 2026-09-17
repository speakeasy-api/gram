import { describe, expect, it, vi } from "vitest";

import {
  formatDailyMeterRate,
  formatMeterQuantity,
  meterAxisTicks,
  meterDateLabel,
  meterPoints,
  type AdminMeterUsage,
} from "./meterUsage";

function date(iso: string): Date {
  return new Date(iso);
}

function meterWindow(from: string, to: string): AdminMeterUsage["window"] {
  return { from: date(from), to: date(to) };
}

function bucket(
  from: string,
  to: string,
  total: string,
): AdminMeterUsage["buckets"][number] {
  return { from: date(from), to: date(to), total };
}

function usage(overrides: Partial<AdminMeterUsage> = {}): AdminMeterUsage {
  return {
    family: "agent_session_storage",
    window: meterWindow("2026-01-01T00:00:00.000Z", "2026-01-04T00:00:00.000Z"),
    billingCycles: [],
    unit: "stokens",
    total: "0",
    buckets: [],
    queriedAt: date("2026-01-04T00:00:00.000Z"),
    measurementMethod: "ordinary usage",
    ...overrides,
  };
}

function inZone<T>(tz: string, body: () => T): T {
  vi.stubEnv("TZ", tz);
  try {
    return body();
  } finally {
    vi.unstubAllEnvs();
  }
}

describe("meterPoints", () => {
  it("keeps exact totals above Number's safe integer range", () => {
    const points = meterPoints(
      usage({
        buckets: [
          bucket(
            "2026-01-01T00:00:00.000Z",
            "2026-01-02T00:00:00.000Z",
            "9007199254740993",
          ),
          bucket("2026-01-02T00:00:00.000Z", "2026-01-03T00:00:00.000Z", "2"),
          bucket("2026-01-03T00:00:00.000Z", "2026-01-04T00:00:00.000Z", "3"),
        ],
      }),
      "monthly",
      true,
    );

    expect(points).toEqual([
      {
        from: "2026-01-01T00:00:00.000Z",
        to: "2026-01-04T00:00:00.000Z",
        total: "9007199254740998",
        value: Number("9007199254740998"),
      },
    ]);
  });

  it("uses Monday-start UTC weeks across a year and clips partial weeks", () => {
    inZone("America/Los_Angeles", () => {
      const points = meterPoints(
        usage({
          window: meterWindow(
            "2025-12-31T00:00:00.000Z",
            "2026-01-07T00:00:00.000Z",
          ),
          queriedAt: date("2026-01-07T00:00:00.000Z"),
          buckets: [
            bucket("2025-12-31T00:00:00.000Z", "2026-01-01T00:00:00.000Z", "2"),
            bucket("2026-01-01T00:00:00.000Z", "2026-01-02T00:00:00.000Z", "3"),
            bucket("2026-01-05T00:00:00.000Z", "2026-01-06T00:00:00.000Z", "7"),
          ],
        }),
        "weekly",
        false,
      );

      expect(points).toEqual([
        {
          from: "2025-12-31T00:00:00.000Z",
          to: "2026-01-05T00:00:00.000Z",
          total: "5",
          value: 5,
        },
        {
          from: "2026-01-05T00:00:00.000Z",
          to: "2026-01-07T00:00:00.000Z",
          total: "7",
          value: 7,
        },
      ]);
    });
  });

  it("clips partial calendar months to the report window", () => {
    const points = meterPoints(
      usage({
        window: meterWindow(
          "2026-01-30T00:00:00.000Z",
          "2026-03-02T00:00:00.000Z",
        ),
        queriedAt: date("2026-03-02T00:00:00.000Z"),
        buckets: [
          bucket("2026-01-30T00:00:00.000Z", "2026-01-31T00:00:00.000Z", "1"),
          bucket("2026-02-01T00:00:00.000Z", "2026-02-02T00:00:00.000Z", "2"),
          bucket("2026-03-01T00:00:00.000Z", "2026-03-02T00:00:00.000Z", "3"),
        ],
      }),
      "monthly",
      false,
    );

    expect(points.map(({ from, to, total }) => ({ from, to, total }))).toEqual([
      {
        from: "2026-01-30T00:00:00.000Z",
        to: "2026-02-01T00:00:00.000Z",
        total: "1",
      },
      {
        from: "2026-02-01T00:00:00.000Z",
        to: "2026-03-01T00:00:00.000Z",
        total: "2",
      },
      {
        from: "2026-03-01T00:00:00.000Z",
        to: "2026-03-02T00:00:00.000Z",
        total: "3",
      },
    ]);
  });

  it("stops a cumulative series at the retrieval instant instead of projecting future days", () => {
    const points = meterPoints(
      usage({
        window: meterWindow(
          "2026-09-01T00:00:00.000Z",
          "2026-09-08T00:00:00.000Z",
        ),
        queriedAt: date("2026-09-03T12:00:00.000Z"),
        buckets: Array.from({ length: 7 }, (_, day) =>
          bucket(
            `2026-09-0${day + 1}T00:00:00.000Z`,
            `2026-09-0${day + 2}T00:00:00.000Z`,
            "1",
          ),
        ),
      }),
      "daily",
      true,
    );

    expect(points).toEqual([
      {
        from: "2026-09-01T00:00:00.000Z",
        to: "2026-09-02T00:00:00.000Z",
        total: "1",
        value: 1,
      },
      {
        from: "2026-09-02T00:00:00.000Z",
        to: "2026-09-03T00:00:00.000Z",
        total: "2",
        value: 2,
      },
      {
        from: "2026-09-03T00:00:00.000Z",
        to: "2026-09-03T12:00:00.000Z",
        total: "3",
        value: 3,
      },
    ]);
  });
});

describe("meter quantity summaries", () => {
  it("formats every token and byte scale with exact integer arithmetic", () => {
    expect([
      formatMeterQuantity("999", "stokens"),
      formatMeterQuantity("1000", "stokens"),
      formatMeterQuantity("1000000", "stokens"),
      formatMeterQuantity("1000000000", "stokens"),
      formatMeterQuantity("1023", "bytes"),
      formatMeterQuantity("1024", "bytes"),
      formatMeterQuantity("1048576", "bytes"),
      formatMeterQuantity("1073741824", "bytes"),
    ]).toEqual([
      "999 tokens",
      "1 KTok",
      "1 MTok",
      "1 BTok",
      "1,023 bytes",
      "1 KiB",
      "1 MiB",
      "1 GiB",
    ]);
    expect(formatMeterQuantity("9007199254740993", "stokens")).toBe(
      "9,007,199.3 BTok",
    );
  });

  it("uses the exact total and only elapsed report time for a daily rate", () => {
    expect(
      formatDailyMeterRate(
        usage({
          total: "9007199254740993",
          window: meterWindow(
            "2026-01-01T00:00:00.000Z",
            "2026-01-03T00:00:00.000Z",
          ),
          queriedAt: date("2026-01-02T12:00:00.000Z"),
        }),
      ),
    ).toBe("6,004,799.5 BTok/day");
  });
});

describe("meterAxisTicks", () => {
  it("keeps tiny and huge integer ranges truthfully distinct", () => {
    expect(meterAxisTicks(0)).toEqual([0, 1]);
    expect(meterAxisTicks(1)).toEqual([0, 1]);

    const ticks = meterAxisTicks(Number("9007199254740993"));
    const labels = ticks.map((tick) =>
      formatMeterQuantity(BigInt(tick).toString(), "stokens"),
    );

    expect(ticks.length).toBeGreaterThan(1);
    expect(ticks.every(Number.isInteger)).toBe(true);
    expect(new Set(labels).size).toBe(labels.length);
  });
});

describe("meterDateLabel", () => {
  it("labels the UTC date independently of the operator's timezone", () => {
    inZone("America/Los_Angeles", () => {
      expect(new Date("2026-01-02T01:00:00.000Z").getDate()).toBe(1);
      expect(meterDateLabel(date("2026-01-02T01:00:00.000Z"))).toBe("Jan 2");
    });
  });
});
