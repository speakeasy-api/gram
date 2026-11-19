// @vitest-environment node
import { describe, expect, it } from "vitest";
import { normalizeGitHubOrg } from "./github";

describe("normalizeGitHubOrg", () => {
  it.each([
    ["example-org", "example-org"],
    ["  example-org  ", "example-org"],
    ["https://github.com/example-org", "example-org"],
    ["https://github.com/example-org/", "example-org"],
    ["github.com/example-org", "example-org"],
    [
      "https://github.com/organizations/example-org/settings/applications",
      "example-org",
    ],
    [
      "https://github.com/orgs/example-org/repositories?type=all",
      "example-org",
    ],
  ])("normalizes %s to %s", (input, expected) => {
    expect(normalizeGitHubOrg(input)).toBe(expected);
  });

  it.each([
    "https://example.com/example-org",
    "https://github.com.evil.example/example-org",
    "https://github.com/",
    "https://github.com/orgs/",
    "https://github.com/organizations/new",
    "https://github.com/example-org/repository",
    "https://github.com/settings/organizations",
  ])("preserves unsupported URLs for validation: %s", (input) => {
    expect(normalizeGitHubOrg(input)).toBe(input);
  });
});
