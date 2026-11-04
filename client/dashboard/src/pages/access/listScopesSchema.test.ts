import { describe, expect, it } from "vitest";
import { ListScopesResult$inboundSchema } from "@gram/client/models/components/listscopesresult.js";

describe("listScopes response schema", () => {
  it("accepts the plugin write permission and its exclusion scope", () => {
    const result = ListScopesResult$inboundSchema.parse({
      scopes: [
        {
          slug: "plugin:write",
          label: "Publish plugins",
          description: "Manage plugin contents and publishing",
          resource_type: "project",
          category: "plugin",
          visibility: "user_visible",
          agent_eligible: true,
          exclusion_scope: "plugin:blocked_write",
          sensitive: false,
          sub_scopes: [],
        },
        {
          slug: "plugin:blocked_write",
          label: "Exception: publish plugins",
          description: "Exclude plugin content changes",
          resource_type: "project",
          category: "plugin",
          visibility: "internal",
          agent_eligible: true,
          sensitive: false,
          sub_scopes: [],
        },
      ],
      tasks: [],
    });

    expect(result.scopes[0]).toMatchObject({
      slug: "plugin:write",
      label: "Publish plugins",
      resourceType: "project",
      category: "plugin",
      exclusionScope: "plugin:blocked_write",
      sensitive: false,
      subScopes: [],
    });
    expect(result.scopes).toHaveLength(2);
  });

  it("carries the sensitivity reason and included scopes", () => {
    const result = ListScopesResult$inboundSchema.parse({
      scopes: [
        {
          slug: "chat:write",
          label: "Manage agent sessions",
          description:
            "Rename, delete, and give feedback on other members' agent sessions.",
          resource_type: "chat",
          category: "session",
          visibility: "user_visible",
          agent_eligible: false,
          sensitive: true,
          sensitivity: "conversations",
          sub_scopes: ["chat:read"],
        },
        // A category or reason the SDK does not know yet must not reject the
        // whole response: both enums are open.
        {
          slug: "log:read",
          label: "View tool logs",
          description: "View tool-call logs.",
          resource_type: "project",
          category: "a_future_category",
          visibility: "user_visible",
          agent_eligible: false,
          sensitive: true,
          sensitivity: "a_future_reason",
          sub_scopes: [],
        },
      ],
      tasks: [
        {
          slug: "session_review",
          label: "Review other members' sessions",
          scopes: ["chat:read", "log:read"],
        },
      ],
    });

    expect(result.scopes[0]).toMatchObject({
      sensitive: true,
      sensitivity: "conversations",
      subScopes: ["chat:read"],
    });
    expect(result.scopes).toHaveLength(2);
    expect(result.tasks).toEqual([
      {
        slug: "session_review",
        label: "Review other members' sessions",
        scopes: ["chat:read", "log:read"],
      },
    ]);
  });
});
