import { describe, expect, it } from "vitest";

import { validateSlugFormat } from "./useMcpEndpointSlugValidation";

describe("validateSlugFormat", () => {
  const slug = (length: number) => "a".repeat(length);

  it("keeps a hosted server's custom-domain slugs to 40 characters", () => {
    expect(validateSlugFormat(slug(40), "domain-1", true)).toBeNull();
    expect(validateSlugFormat(slug(41), "domain-1", true)).toBe(
      "Must be 40 characters or fewer",
    );
  });

  it("allows 128 characters on a custom domain for other servers", () => {
    expect(validateSlugFormat(slug(128), "domain-1", false)).toBeNull();
    expect(validateSlugFormat(slug(129), "domain-1", false)).toBe(
      "Must be 128 characters or fewer",
    );
  });
});
