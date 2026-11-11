import { useQueries } from "@tanstack/react-query";
import { z } from "zod";
import type { InspectTool } from "../inspect/types";
import { bareToolName } from "../inspect/useToolHealth";
import { gatewayRPC } from "./useGatewayInspection";

const describedServerSchema = z.object({
  structuredContent: z
    .object({
      tools: z.array(
        z.object({ name: z.string(), description: z.string().optional() }),
      ),
    })
    .optional(),
  isError: z.boolean().optional(),
  content: z.array(z.object({ text: z.string().optional() })).optional(),
});

const describedToolsSchema = z.object({
  structuredContent: z
    .object({
      tools: z.array(
        z
          .object({
            name: z.string(),
            description: z.string().optional(),
            inputSchema: z.unknown().optional(),
            annotations: z
              .object({
                title: z.string().optional(),
                readOnlyHint: z.boolean().optional().catch(undefined),
                destructiveHint: z.boolean().optional().catch(undefined),
                idempotentHint: z.boolean().optional().catch(undefined),
                openWorldHint: z.boolean().optional().catch(undefined),
              })
              .optional()
              .catch(undefined),
          })
          .passthrough(),
      ),
    })
    .optional(),
});

type DescribedTool = NonNullable<
  z.infer<typeof describedToolsSchema>["structuredContent"]
>["tools"][number];

export interface GatewayMemberTools {
  slug: string;
  name: string;
  tools: InspectTool[];
  /** What the gateway said when this member's catalog couldn't be read. */
  error?: string;
  isLoading: boolean;
}

/**
 * The real tools behind a Progressive gateway, read the way an agent would:
 * describe_server for each member's names, then describe_tools for their input
 * schemas. One query per member, so a member outage only blanks that member.
 */
export function useGatewayMemberTools(
  mcpUrl: string | undefined,
  headers: Record<string, string> | undefined,
  servers: { slug: string; name?: string }[] | undefined,
  options: { enabled: boolean; cacheKey: string },
): GatewayMemberTools[] {
  const members = servers ?? [];
  const results = useQueries({
    queries: members.map((server) => ({
      queryKey: ["gatewayMemberTools", mcpUrl, server.slug, options.cacheKey],
      enabled: options.enabled && !!mcpUrl,
      retry: false,
      staleTime: 5 * 60 * 1000,
      throwOnError: false,
      queryFn: async (): Promise<{ tools: InspectTool[]; error?: string }> => {
        if (!mcpUrl) throw new Error("No gateway URL configured");
        const described = describedServerSchema.parse(
          await gatewayRPC(mcpUrl, headers, "tools/call", {
            name: "describe_server",
            arguments: { server: server.slug },
          }),
        );
        if (described.isError || !described.structuredContent) {
          return {
            tools: [],
            error:
              described.content?.[0]?.text ??
              "The gateway couldn't describe this server.",
          };
        }
        const names = described.structuredContent.tools.map((t) => t.name);
        if (names.length === 0) return { tools: [] };

        // Schemas are best-effort: without them the tools still list, and
        // the run form falls back to "no arguments".
        let schemas = new Map<string, DescribedTool>();
        try {
          const detailed = describedToolsSchema.parse(
            await gatewayRPC(mcpUrl, headers, "tools/call", {
              name: "describe_tools",
              arguments: { tools: names },
            }),
          );
          schemas = new Map(
            (detailed.structuredContent?.tools ?? []).map((t) => [t.name, t]),
          );
        } catch {
          // Keep the names-only listing.
        }

        return {
          tools: described.structuredContent.tools.map((tool) => {
            const detail = schemas.get(tool.name);
            return {
              name: tool.name,
              label: bareToolName(tool.name),
              description: detail?.description ?? tool.description,
              inputSchema: detail?.inputSchema,
              annotations: detail?.annotations,
              group: server.name ?? server.slug,
            };
          }),
        };
      },
    })),
  });

  return members.map((server, index) => {
    const result = results[index];
    return {
      slug: server.slug,
      name: server.name ?? server.slug,
      tools: result?.data?.tools ?? [],
      error:
        result?.data?.error ??
        (result?.error instanceof Error ? result.error.message : undefined),
      isLoading: !!result?.isLoading,
    };
  });
}
