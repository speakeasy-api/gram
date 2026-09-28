import { describe, expect, it } from "vitest";
import { accountFact, methodAccountFact, methodEligibility } from "./accounts";
import {
  notApplicable,
  type Eligibility,
  type Fact,
  type Method,
  type PlatformSupport,
} from "./model";

const supported: Fact = {
  status: "supported",
  note: "via hooks",
  verify: false,
};
const support = (
  personal: Eligibility,
  team: Eligibility = "supported",
  enterprise: Eligibility = "supported",
): PlatformSupport => ({
  platform: "cli",
  applicability: "applicable",
  accounts: { personal, team, enterprise },
  note: "",
  cells: { session: supported },
});

describe("accountFact", () => {
  it("leaves the cell alone for every account type and for eligible ones", () => {
    expect(accountFact(support("unsupported"), supported, "all")).toEqual(
      supported,
    );
    expect(accountFact(support("unsupported"), supported, "team")).toEqual(
      supported,
    );
  });
  it("makes the capability impossible for an ineligible account type", () => {
    const fact = accountFact(support("unsupported"), supported, "personal");
    expect(fact.status).toBe("impossible");
    expect(fact.note).toBe(
      "Personal accounts are not eligible for this method",
    );
  });
  it("leaves unknown eligibility unknown and to be verified", () => {
    const fact = accountFact(support("unknown"), supported, "personal");
    expect(fact.status).toBe("unknown");
    expect(fact.verify).toBe(true);
    expect(accountFact(undefined, supported, "team").status).toBe("unknown");
  });
  it("never turns not applicable into anything else", () => {
    expect(
      accountFact(support("unsupported"), notApplicable, "personal"),
    ).toEqual(notApplicable);
  });
});

describe("methodEligibility", () => {
  const method = (...accounts: Eligibility[]): Method => ({
    id: "hooks",
    name: "Hooks",
    vendor: "Vendor",
    plans: "",
    claims: { session: supported },
    platforms: accounts.map((personal, index) => ({
      ...support(personal),
      platform: `p${index}`,
    })),
  });
  it("is eligible where any platform is, unknown where none is but some might be", () => {
    expect(
      methodEligibility(method("unsupported", "supported"), "personal"),
    ).toBe("supported");
    expect(
      methodEligibility(method("unsupported", "unknown"), "personal"),
    ).toBe("unknown");
    expect(
      methodEligibility(method("unsupported", "unsupported"), "personal"),
    ).toBe("unsupported");
    expect(methodEligibility(method(), "personal")).toBe("unsupported");
  });
  it("narrows a method's claim the same way", () => {
    expect(
      methodAccountFact(method("unsupported"), supported, "personal").status,
    ).toBe("impossible");
    expect(methodAccountFact(method("unsupported"), supported, "all")).toEqual(
      supported,
    );
  });
});
