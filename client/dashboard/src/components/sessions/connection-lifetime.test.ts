import { describe, expect, it } from "vitest";
import {
  connectionLifetime,
  connectionLifetimeLabel,
  lifetimeEndingSoon,
  soonestLifetime,
} from "./connection-lifetime";

const now = Date.parse("2026-10-23T12:00:00Z");
const hours = (n: number) => new Date(now + n * 3_600_000);

describe("connectionLifetime", () => {
  it("reads a refresh token with no expiry as having no end", () => {
    const lifetime = connectionLifetime(
      { accessExpiresAt: hours(1), hasRefreshToken: true },
      now,
    );
    expect(lifetime).toEqual({ kind: "refreshes" });
    expect(connectionLifetimeLabel(lifetime, now)).toBe(
      "Refreshes itself, no end date",
    );
  });

  it("ends a refreshing connection at its refresh token's expiry", () => {
    const lifetime = connectionLifetime(
      {
        accessExpiresAt: hours(1),
        refreshExpiresAt: hours(24 * 30),
        hasRefreshToken: true,
      },
      now,
    );
    expect(lifetime).toEqual({
      kind: "refreshes-until",
      at: hours(24 * 30),
      soon: false,
    });
    expect(connectionLifetimeLabel(lifetime, now)).toMatch(
      /^Refreshes itself until /,
    );
  });

  it("ends a connection without a refresh token at its access expiry", () => {
    const lifetime = connectionLifetime(
      { accessExpiresAt: hours(5), hasRefreshToken: false },
      now,
    );
    expect(lifetime).toEqual({ kind: "expires", at: hours(5), soon: true });
    expect(lifetimeEndingSoon(lifetime)).toBe(true);
    expect(connectionLifetimeLabel(lifetime, now)).toMatch(
      /^Expires .* \(in about 5 hours\) and does not refresh$/,
    );
  });

  it("falls back to the access token once the refresh token has lapsed", () => {
    expect(
      connectionLifetime(
        {
          accessExpiresAt: hours(2),
          refreshExpiresAt: hours(-1),
          hasRefreshToken: true,
        },
        now,
      ),
    ).toEqual({ kind: "expires", at: hours(2), soon: true });
  });

  it("reports a connection whose last token has expired as ended", () => {
    const lifetime = connectionLifetime(
      {
        accessExpiresAt: hours(-3),
        refreshExpiresAt: hours(-2),
        hasRefreshToken: true,
      },
      now,
    );
    expect(lifetime).toEqual({ kind: "ended", at: hours(-3) });
    expect(lifetimeEndingSoon(lifetime)).toBe(false);
    expect(connectionLifetimeLabel(lifetime, now)).toBe(
      "Ended about 3 hours ago",
    );
  });

  it("says nothing about an end it cannot read", () => {
    const lifetime = connectionLifetime(
      {
        accessExpiresAt: "not a date" as unknown as Date,
        hasRefreshToken: false,
      },
      now,
    );
    expect(lifetime).toEqual({ kind: "unknown" });
    expect(connectionLifetimeLabel(lifetime, now)).toBe("Expiry not recorded");
  });
});

describe("soonestLifetime", () => {
  it("picks the earliest deadline and ranks no deadline last", () => {
    expect(
      soonestLifetime([
        { kind: "refreshes" },
        { kind: "expires", at: hours(48), soon: false },
        { kind: "refreshes-until", at: hours(10), soon: true },
      ]),
    ).toEqual({ kind: "refreshes-until", at: hours(10), soon: true });
    expect(soonestLifetime([])).toBeUndefined();
  });
});
