type KeyName = { id: string; name: string };
export type AgentKeyName = KeyName & { agentId: string };

/** Keep palette discovery from flooding the API for a large agent roster. */
const KEY_LOOKUP_CONCURRENCY = 4;

export async function fetchAgentKeyNames(
  agentIds: string[],
  list: (agentId: string) => Promise<{ keys: KeyName[] }>,
  signal: AbortSignal,
): Promise<AgentKeyName[]> {
  const rows: AgentKeyName[] = [];
  for (
    let offset = 0;
    offset < agentIds.length;
    offset += KEY_LOOKUP_CONCURRENCY
  ) {
    signal.throwIfAborted();
    const batch = await Promise.all(
      agentIds
        .slice(offset, offset + KEY_LOOKUP_CONCURRENCY)
        .map(async (agentId) => {
          const result = await list(agentId);
          // Only names and routing IDs belong in palette caches, never secrets.
          return result.keys.map(({ id, name }) => ({ agentId, id, name }));
        }),
    );
    rows.push(...batch.flat());
  }
  return rows;
}
