import { render } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { ExternalMcpIcon } from "./MCPToolsetIcon";

vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: { servers: [{ registrySpecifier: "example.test/server" }] },
  }),
}));
vi.mock("@gram/client/react-query/latestDeployment.js", () => ({
  useLatestDeployment: () => ({
    data: {
      deployment: {
        externalMcps: [
          { slug: "server", registryServerSpecifier: "example.test/server" },
        ],
      },
    },
  }),
}));

it("keeps the generic icon when a matching catalog entry has no logo", () => {
  const { container } = render(
    <ExternalMcpIcon
      externalMcpSlug="server"
      alt="Server"
      fallbackClassName="size-6"
    />,
  );
  expect(container.querySelector("svg.size-6")).not.toBeNull();
  expect(container.querySelector("img")).toBeNull();
});
