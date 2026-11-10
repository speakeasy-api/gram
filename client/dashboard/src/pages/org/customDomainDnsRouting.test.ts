import { expect, it } from "vitest";

import { showRecommendedDnsUpdate } from "./customDomainDnsRouting";

const activeWithTarget = {
  verified: true,
  activated: true,
  dnsTarget: "abcdefgh23.domains.example.net",
};

it.each([
  { dnsRoutingMethod: "shared_cname", expected: true },
  // A flattened CNAME to the designated target reads as A records too.
  { dnsRoutingMethod: "a_record", expected: false },
  { dnsRoutingMethod: "per_domain_cname", expected: false },
  { dnsRoutingMethod: "unknown", expected: false },
  { dnsRoutingMethod: undefined, expected: false },
])(
  "recommends the designated target to a domain observed as $dnsRoutingMethod: $expected",
  ({ dnsRoutingMethod, expected }) => {
    expect(
      showRecommendedDnsUpdate({ ...activeWithTarget, dnsRoutingMethod }),
    ).toBe(expected);
  },
);

it.each([
  { name: "without a designated target", domain: { dnsTarget: undefined } },
  { name: "before verification", domain: { verified: false } },
  // Health checks clear the recommendation and only run once activated.
  { name: "before activation", domain: { activated: false } },
])("does not recommend an update $name", ({ domain }) => {
  expect(
    showRecommendedDnsUpdate({
      ...activeWithTarget,
      dnsRoutingMethod: "shared_cname",
      ...domain,
    }),
  ).toBe(false);
});
