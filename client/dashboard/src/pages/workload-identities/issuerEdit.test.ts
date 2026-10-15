import { describe, expect, it } from "vitest";
import { changedIssuerFields, issuerValuesDiffer } from "./issuerEdit";

const stored = {
  name: "Example CI",
  description: "Deploy jobs",
  issuer: "https://ci-identity.example.com",
  jwksUri: "https://ci-identity.example.com/jwks",
  tags: ["production", "ci"],
};

describe("changedIssuerFields", () => {
  it("is empty when nothing changed", () => {
    expect(changedIssuerFields(stored, { ...stored })).toEqual({});
    expect(issuerValuesDiffer(stored, { ...stored })).toBe(false);
  });

  it("ignores surrounding whitespace, as the server trims it", () => {
    expect(
      changedIssuerFields(stored, { ...stored, name: "  Example CI " }),
    ).toEqual({});
  });

  it("includes each changed field, trimmed", () => {
    expect(
      changedIssuerFields(stored, {
        ...stored,
        name: " Example deploys ",
        jwksUri: "https://keys.example.com/jwks ",
        tags: ["ci", "production"],
      }),
    ).toEqual({
      name: "Example deploys",
      jwksUri: "https://keys.example.com/jwks",
      tags: ["ci", "production"],
    });
  });

  it("sends a cleared description and cleared tags", () => {
    expect(
      changedIssuerFields(stored, { ...stored, description: "  ", tags: [] }),
    ).toEqual({ description: "", tags: [] });
  });

  it("never includes the issuer URL", () => {
    expect(
      changedIssuerFields(stored, {
        ...stored,
        issuer: "https://other.example.com",
      }),
    ).toEqual({});
  });
});
