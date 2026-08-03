import { describe, expect, it } from "vitest";
import { collectionInstallDisabledReason } from "./install-availability";

describe("collectionInstallDisabledReason", () => {
  it("explains each unavailable state in priority order", () => {
    expect(
      collectionInstallDisabledReason({
        isLoading: true,
        installableServerCount: 0,
        projectCount: 0,
      }),
    ).toBe("Checking whether this collection can be installed");
    expect(
      collectionInstallDisabledReason({
        isLoading: false,
        installableServerCount: 0,
        projectCount: 0,
      }),
    ).toBe("This collection has no servers with active endpoints to install");
    expect(
      collectionInstallDisabledReason({
        isLoading: false,
        installableServerCount: 1,
        projectCount: 0,
      }),
    ).toBe("Create a project before installing this collection");
  });

  it("enables installation when a server and project are available", () => {
    expect(
      collectionInstallDisabledReason({
        isLoading: false,
        installableServerCount: 1,
        projectCount: 1,
      }),
    ).toBeNull();
  });
});
