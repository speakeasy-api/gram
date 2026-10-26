import { expect, it } from "vitest";
import {
  deliveryErrorMessage,
  deliveryStatus,
  promotionReason,
} from "./registryPromotionLabels";

it("distinguishes missing delivery state from unknown server state", () => {
  expect(deliveryStatus(undefined)).toBe("Pending");
  expect(deliveryStatus("pending")).toBe("Pending");
  expect(deliveryStatus("retrying")).toBe("Retry scheduled");
  expect(deliveryStatus("future_status")).toBe("Unknown status");
  expect(deliveryStatus("")).toBe("Unknown status");
});
it.each(["constructor", "toString", "__proto__"])(
  "uses safe fallback labels for %s",
  (code) => {
    expect(promotionReason(code)).toBe(
      "This server is not ready for promotion",
    );
    expect(deliveryStatus(code)).toBe("Unknown status");
    expect(deliveryErrorMessage(code)).toBe("Guide delivery needs attention.");
  },
);
