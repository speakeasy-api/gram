// Keep an explicit valid choice, or choose the only available client. Reusing an
// issuer should also reuse its sole client without asking for a redundant second
// selection. Multiple clients remain an explicit operator choice.
export function selectExistingClient<T extends { id: string }>(
  clients: T[],
  selectedClientId: string,
  autoSelectSoleClient: boolean,
): T | undefined {
  return (
    clients.find((client) => client.id === selectedClientId) ??
    (autoSelectSoleClient && clients.length === 1 ? clients[0] : undefined)
  );
}
