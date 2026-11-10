import { z } from "zod";

// `entry` opens the debugger on a registry entry's remote; `url` holds the
// URL last traced, so a trace can be shared or reloaded. Both are optional:
// with neither, this is the one-off debugger.
const debugSearchSchema = z.object({
  entry: z.string().uuid().optional().catch(undefined),
  url: z.string().min(1).optional().catch(undefined),
});

export type RegistryDebugSearch = z.infer<typeof debugSearchSchema>;

export function registryDebugSearch(
  search: Record<string, unknown>,
): RegistryDebugSearch {
  return debugSearchSchema.parse(search);
}
