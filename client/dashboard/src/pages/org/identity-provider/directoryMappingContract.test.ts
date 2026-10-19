import { afterEach, describe, expect, it } from "vitest";
import {
  completeCreateRoleFlow,
  pendingMappingFromParams,
  startCreateRoleFlow,
} from "./directoryMappingFlow";

import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";

afterEach(() => window.sessionStorage.clear());

function recordingClient() {
  const requests: Request[] = [];
  const client = new Gram({
    serverURL: "https://gram.example",
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        requests.push(request as Request);
        return new Response(
          JSON.stringify({
            name: "conflict",
            message: "Refresh before retrying",
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        );
      },
    }),
  });
  return { client, requests };
}

describe("directory mapping SDK contract", () => {
  it.each(["chip edit", "create-role return"])(
    "sends a %s as a guarded set write and surfaces conflicts",
    async (flow) => {
      const { client, requests } = recordingClient();
      const source = {
        sourceKind: "attribute" as const,
        attributeKey: "department",
        attributeValue: "Engineering",
      };
      const roleUrn = "role:global:admin";
      const previous = "role:organization:previous";
      const form =
        flow === "chip edit"
          ? {
              ...source,
              roleUrns: [previous, roleUrn],
              expectedRoleUrns: [previous],
            }
          : pendingMappingFromParams(
              completeCreateRoleFlow(
                startCreateRoleFlow(source, "Engineering"),
                roleUrn,
              ),
              [
                {
                  id: "mapping-1",
                  ...source,
                  roleUrn: previous,
                  createdAt: new Date(0),
                  updatedAt: new Date(0),
                },
              ],
            )!.form;
      await expect(
        client.access.setDirectoryRoleMappings({
          setDirectoryRoleMappingsForm: form,
        }),
      ).rejects.toMatchObject({ statusCode: 409 });
      expect(requests).toHaveLength(1);
      expect(new URL(requests[0]!.url).pathname).toBe(
        "/rpc/access.setDirectoryRoleMappings",
      );
      expect(await requests[0]!.json()).toEqual({
        source_kind: "attribute",
        attribute_key: "department",
        attribute_value: "Engineering",
        role_urns: [previous, roleUrn],
        expected_role_urns: [previous],
      });
    },
  );

  it.each([undefined, [], ["role:global:admin"]])(
    "preserves optional expected sets on the wire: %j",
    async (expectedRoleUrns) => {
      const { client, requests } = recordingClient();
      await expect(
        client.access.setDirectoryRoleMappings({
          setDirectoryRoleMappingsForm: {
            sourceKind: "attribute",
            attributeKey: "department",
            attributeValue: "Engineering",
            roleUrns: [],
            expectedRoleUrns,
          },
        }),
      ).rejects.toMatchObject({ statusCode: 409 });
      const body = await requests[0]!.json();
      if (expectedRoleUrns === undefined)
        expect(body).not.toHaveProperty("expected_role_urns");
      else expect(body.expected_role_urns).toEqual(expectedRoleUrns);
    },
  );
});
