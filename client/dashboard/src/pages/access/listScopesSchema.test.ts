import { describe, expect, it } from "vitest";
import { ListScopesResult$inboundSchema } from "@gram/client/models/components/listscopesresult.js";

describe("listScopes response schema", () => {
  it("accepts the plugin write permission and its exclusion scope", () => {
    const result = ListScopesResult$inboundSchema.parse({
      scopes: [
        {
          slug: "plugin:write",
          description: "Manage plugin contents and publishing",
          resource_type: "project",
          visibility: "user_visible",
          agent_eligible: true,
          exclusion_scope: "plugin:blocked_write",
        },
        {
          slug: "plugin:blocked_write",
          description: "Exclude plugin content changes",
          resource_type: "project",
          visibility: "internal",
          agent_eligible: true,
        },
      ],
    });

    expect(result.scopes[0]).toMatchObject({
      slug: "plugin:write",
      resourceType: "project",
      exclusionScope: "plugin:blocked_write",
    });
    expect(result.scopes).toHaveLength(2);
  });
});
