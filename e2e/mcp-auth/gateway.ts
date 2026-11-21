import type { FixtureMcpClient } from "./client.ts";

type Driver = Pick<FixtureMcpClient, "listTools" | "callTool">;
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid gateway result");
  return value as Record<string, unknown>;
}
function payload(value: unknown): Record<string, unknown> {
  const result = record(value);
  if (result.isError === true)
    throw new Error("Gateway discovery returned an error result");
  if (result.structuredContent) return record(result.structuredContent);
  if (Array.isArray(result.content)) {
    const text = result.content
      .map(record)
      .find((chunk) => chunk.type === "text");
    if (typeof text?.text === "string") return record(JSON.parse(text.text));
  }
  throw new Error("Gateway discovery did not return structured content");
}

export type GatewayInventoryObservation = {
  progressive: boolean;
  expected: {
    member: number;
    present: boolean;
    hidden: boolean;
    status: string | null;
  }[];
  otherMemberCount: number;
  otherHiddenCount: number;
};

function matchesSlug(value: unknown, slug: string): boolean {
  return (
    typeof value === "string" && (value === slug || value.endsWith(`.${slug}`))
  );
}
function unique<T>(rows: T[], slug: string): T | undefined {
  if (rows.length > 1)
    throw new Error(
      `GATEWAY_CATALOG_FAILURE: fixture member ${slug} is ambiguous`,
    );
  return rows[0];
}

/** Follow the real discovery contract; progressive gateways do not list member tools directly. */
export async function gatewayEchoes(
  driver: Driver,
  expectedSlugs: string[],
  observe?: (inventory: GatewayInventoryObservation) => Promise<void>,
) {
  const { tools } = await driver.listTools();
  const progressive = tools.some((tool) => tool.name === "list_servers");
  const inventory = progressive
    ? payload(await driver.callTool("list_servers"))
    : { servers: [], hidden: [] };
  const members = inventory.servers;
  if (progressive && !Array.isArray(members))
    throw new Error("Invalid gateway member list");
  if (inventory.hidden !== undefined && !Array.isArray(inventory.hidden))
    throw new Error("Invalid gateway hidden member list");
  const rows = (members as unknown[]).map(record);
  const hidden = ((inventory.hidden ?? []) as unknown[]).map(record);
  const membersBySlug = expectedSlugs.map((slug) =>
    unique(
      rows.filter((row) => matchesSlug(row.slug, slug)),
      slug,
    ),
  );
  const hiddenBySlug = expectedSlugs.map((slug) =>
    unique(
      hidden.filter((row) => matchesSlug(row.slug, slug)),
      slug,
    ),
  );
  const directBySlug = expectedSlugs.map((slug) =>
    unique(
      tools.filter(
        (tool) =>
          tool.name.endsWith("--fixture_echo") &&
          matchesSlug(tool.name.slice(0, -"--fixture_echo".length), slug),
      ),
      slug,
    ),
  );
  if (observe) {
    const matches = (row: Record<string, unknown>, slug: string) =>
      matchesSlug(row.slug, slug);
    await observe({
      progressive,
      expected: expectedSlugs.map((slug, index) => {
        const row = membersBySlug[index];
        return {
          member: index + 1,
          present: progressive
            ? row !== undefined
            : directBySlug[index] !== undefined,
          hidden: hiddenBySlug[index] !== undefined,
          status:
            row &&
            ["unknown", "available", "unavailable"].includes(String(row.status))
              ? String(row.status)
              : null,
        };
      }),
      otherMemberCount: rows.filter(
        (row) => !expectedSlugs.some((slug) => matches(row, slug)),
      ).length,
      otherHiddenCount: hidden.filter(
        (row) => !expectedSlugs.some((slug) => matches(row, slug)),
      ).length,
    });
  }
  const calls = [];
  for (const [index, slug] of expectedSlugs.entries()) {
    let qualifiedName: string | undefined;
    if (progressive) {
      const member = membersBySlug[index];
      if (!member || typeof member.slug !== "string")
        throw new Error(
          `GATEWAY_FILTER_REGRESSION: fixture member ${slug} is missing`,
        );
      const catalog = payload(
        await driver.callTool("describe_server", { server: member.slug }),
      );
      if (!Array.isArray(catalog.tools))
        throw new Error("Invalid member catalog");
      const echo = catalog.tools
        .map(record)
        .find(
          (tool) =>
            typeof tool.name === "string" &&
            tool.name.endsWith("--fixture_echo"),
        );
      if (typeof echo?.name === "string") qualifiedName = echo.name;
      if (qualifiedName) {
        const schemas = payload(
          await driver.callTool("describe_tools", { tools: [qualifiedName] }),
        );
        if (
          !Array.isArray(schemas.tools) ||
          !schemas.tools.map(record).some((tool) => tool.name === qualifiedName)
        ) {
          throw new Error(
            "Gateway failed to describe the selected fixture tool",
          );
        }
      }
    } else {
      qualifiedName = directBySlug[index]?.name;
    }
    if (!qualifiedName)
      throw new Error(
        `GATEWAY_CATALOG_FAILURE: fixture member ${slug} has no echo tool`,
      );
    const name = qualifiedName;
    calls.push({
      server: slug,
      qualifiedName: name,
      call: (args: Record<string, unknown>) =>
        progressive
          ? driver.callTool("execute_tool", { name, arguments: args })
          : driver.callTool(name, args),
    });
  }
  return calls;
}
