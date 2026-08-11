/**
 * Echoes the caller identity the runner forwards as the second argument, so
 * tests can assert exactly what reaches user code.
 *
 * @param {{name: string, input: unknown}} _call
 * @param {{clientInfo?: {name: string, version: string}, oauthClientId?: string}} [options]
 */
export async function handleToolCall(_call, options) {
  return new Response(JSON.stringify(options ?? null), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
