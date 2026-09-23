import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { usePluginQueryScope } from "./usePluginQueryScope";
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-uuid", slug: "project-slug" }),
  useSession: () => ({ session: "session-a" }),
}));
it("sends the project slug, never the authorization resource ID, on generated SDK requests", async () => {
  const requests: Request[] = [];
  const client = new Gram({
    serverURL: "https://gram.example",
    httpClient: new HTTPClient({
      fetcher: async (request) => {
        requests.push(request as Request);
        return new Response(JSON.stringify({ plugins: [] }), {
          headers: { "Content-Type": "application/json" },
        });
      },
    }),
  });
  const { result } = renderHook(usePluginQueryScope);
  await client.plugins.listPlugins(result.current);
  expect(requests).toHaveLength(1);
  expect(requests[0]!.headers.get("Gram-Project")).toBe("project-slug");
  expect(requests[0]!.headers.get("Gram-Session")).toBe("session-a");
});
