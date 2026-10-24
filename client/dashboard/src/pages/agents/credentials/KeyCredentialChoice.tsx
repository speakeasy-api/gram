import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { clientTypeLabel } from "@/lib/auth-clients";
import { useMcpServerAuthClientChoices } from "@gram/client/react-query/mcpServerAuthClientChoices.js";
import type { JSX } from "react";

/**
 * Which of a server's shared credentials the key uses there. A server with
 * one or none needs no choice, so nothing is shown; with several the key
 * cannot be issued until one is picked.
 */
export function KeyCredentialChoice({
  serverId,
  serverName,
  value,
  onChange,
}: {
  serverId: string;
  serverName: string;
  value: string | undefined;
  onChange: (authClientId: string) => void;
}): JSX.Element | null {
  const choices = useMcpServerAuthClientChoices(
    { mcpServerId: serverId },
    undefined,
    { retry: false, throwOnError: false },
  );
  const options = choices.data?.choices ?? [];
  if (options.length < 2) return null;
  return (
    <Select value={value ?? ""} onValueChange={onChange}>
      <SelectTrigger
        aria-label={`Credential the key uses on ${serverName}`}
        className="mt-2 w-full max-w-sm"
      >
        <SelectValue placeholder="Choose which credential this key uses" />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option.id} value={option.id}>
            {option.name} · {clientTypeLabel(option.clientType)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
