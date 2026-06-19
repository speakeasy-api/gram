import { describe, expect, it } from "vitest";

import { shouldRenderWireUserSessionIssuerModal } from "./rendering";

describe("shouldRenderWireUserSessionIssuerModal", () => {
  it("keeps an already-open modal mounted when the parent hides the entry point", () => {
    expect(
      shouldRenderWireUserSessionIssuerModal({
        showWireUserSessionIssuer: false,
        isOpen: true,
      }),
    ).toBe(true);
  });

  it("unmounts the modal when it is closed and the entry point is hidden", () => {
    expect(
      shouldRenderWireUserSessionIssuerModal({
        showWireUserSessionIssuer: false,
        isOpen: false,
      }),
    ).toBe(false);
  });
});
