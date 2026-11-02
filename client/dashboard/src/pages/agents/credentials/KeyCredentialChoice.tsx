import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { clientTypeLabel } from "@/lib/auth-clients";
import { useMcpServerAuthClientChoices } from "@gram/client/react-query/mcpServerAuthClientChoices.js";
import { Text } from "@/components/ui/Text";
import { useEffect, type JSX } from "react";
/**
 * Which of a server's shared credentials the key uses there. A server with
 * one or none needs no choice, so nothing is shown; with several the key
 * cannot be issued until one is picked.
 *
 * It renders its own wrapper, so a caller's spacing appears only when there
 * is something to show: an empty padded box would grow the row it sits in.
 */
export function KeyCredentialChoice({
  serverId,
  serverName,
  value,
  onChange,
  className,
}: {
  serverId: string;
  serverName: string;
  value: string | undefined;
  onChange: (authClientId: string) => void;
  className?: string;
}): JSX.Element | null {
  const choices = useMcpServerAuthClientChoices(
    { mcpServerId: serverId },
    undefined,
    { retry: false, throwOnError: false },
  );
  const options = choices.data?.choices ?? [];
  const offered = !value || options.some((option) => option.id === value);
  useEffect(() => {
    // A pick the server stopped offering is cleared so the picker asks again.
    if (choices.isSuccess && !offered) onChange("");
  }, [choices.isSuccess, offered, onChange]);
  if (choices.isError) {
    return (
      <div className={className}>
        <Text small role="alert" className="text-destructive mt-2 block">
          The credentials {serverName} offers could not be loaded. Refresh the
          page to try again.
        </Text>
      </div>
    );
  }
  if (options.length < 2) return null;
  return (
    <div className={className}>
      <Select value={offered ? (value ?? "") : ""} onValueChange={onChange}>
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
    </div>
  );
}
