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
import { ACCOUNT_CHOICE } from "./keyCredentialChoices";
/**
 * Which of a server's shared credentials the key uses there. A server with
 * none needs no choice, so nothing is shown. Otherwise the credential is
 * picked here: a key that names none is never served a shared credential. A
 * server that offers only shared credentials cannot be issued a key until one
 * is picked; one that also offers accounts keeps using the connected account
 * unless a shared credential is picked.
 *
 * With `accounts`, the server also signs in a connected account, and that
 * account is listed as a choice of its own (`ACCOUNT_CHOICE`) beside the
 * shared credentials. With `pickSole`, a caller that knows the server signs
 * in no accounts has a single shared credential picked and named rather than
 * asked; without it the reader picks it, as before.
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
  accounts = false,
  pickSole = false,
  label = "Choose which credential this key uses",
}: {
  serverId: string;
  serverName: string;
  value: string | undefined;
  onChange: (authClientId: string) => void;
  className?: string;
  /** The server also signs in an account connected for the agent. */
  accounts?: boolean;
  /**
   * Pick a single shared credential without asking. Only for a caller that
   * knows `accounts`: a server that also signs in accounts keeps them.
   */
  pickSole?: boolean;
  /** The placeholder, before anything is chosen. */
  label?: string;
}): JSX.Element | null {
  const choices = useMcpServerAuthClientChoices(
    { mcpServerId: serverId },
    undefined,
    { retry: false, throwOnError: false },
  );
  const options = choices.data?.choices ?? [];
  const offered =
    !value ||
    (accounts && value === ACCOUNT_CHOICE) ||
    options.some((option) => option.id === value);
  const sole =
    pickSole && !accounts && options.length === 1 ? options[0] : undefined;
  useEffect(() => {
    // A pick the server stopped offering is cleared so the picker asks again.
    if (choices.isSuccess && !offered) {
      onChange("");
      return;
    }
    // Nothing to choose between: the one credential is the choice.
    if (sole && value !== sole.id) onChange(sole.id);
  }, [choices.isSuccess, offered, sole, value, onChange]);
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
  if (options.length === 0) return null;
  if (sole) {
    return (
      <div className={className}>
        <Text small muted className="mt-2 block">
          Uses {sole.name} · {clientTypeLabel(sole.clientType)}
        </Text>
      </div>
    );
  }
  return (
    <div className={className}>
      <Select value={offered ? (value ?? "") : ""} onValueChange={onChange}>
        <SelectTrigger
          aria-label={`Credential the key uses on ${serverName}`}
          className="mt-2 w-full max-w-sm"
        >
          <SelectValue placeholder={label} />
        </SelectTrigger>
        <SelectContent>
          {accounts && (
            <SelectItem value={ACCOUNT_CHOICE}>
              The account connected for this agent
            </SelectItem>
          )}
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
