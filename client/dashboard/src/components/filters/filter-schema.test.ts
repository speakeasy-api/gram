// @vitest-environment node
import { expect, it } from "vitest";
import { chipLabel, customDateRangeBounds } from "./filter-schema";

it("uses the persisted calendar boundary, including a daylight-saving final day", () => {
  const from = new Date("2026-11-01T05:00:00Z");
  const calendarEnd = new Date("2026-11-02T06:00:00Z");
  expect(
    customDateRangeBounds({
      preset: null,
      customRange: { from, to: from },
      customLabel: null,
      calendarEnd,
    }),
  ).toEqual({ from, to: calendarEnd });
});

it("keeps exact custom timestamps and leaves relative ranges unresolved", () => {
  const from = new Date("2026-10-01T10:30:00Z");
  const to = new Date("2026-10-01T11:45:00Z");
  expect(
    customDateRangeBounds({
      preset: null,
      customRange: { from, to },
      customLabel: "Selected interval",
    }),
  ).toEqual({ from, to });
  expect(
    customDateRangeBounds({
      preset: "1d",
      customRange: null,
      customLabel: null,
    }),
  ).toBeNull();
  expect(customDateRangeBounds(undefined)).toBeNull();
});

it("names the dimension on a chip only when the schema opts in", () => {
  const options = [{ value: "a", label: "Linear" }];
  const plain = { id: "s", label: "Service", kind: "select" } as const;
  const prefixed = { ...plain, chipPrefix: true } as const;
  expect(chipLabel(plain, "a", options)).toBe("Linear");
  expect(chipLabel(prefixed, "a", options)).toBe("Service: Linear");
  expect(chipLabel(prefixed, null, options)).toBe("All services");
  const counted = {
    id: "m",
    label: "Message",
    kind: "multiselect",
    chipCount: true,
  } as const;
  expect(chipLabel(counted, ["x", "y", "z"])).toBe("Message: 3");
  expect(chipLabel(counted, [])).toBe("All messages");
});
