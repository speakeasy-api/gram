import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";

export interface ConnectStepMember {
  slug: string;
  name: string;
  mcpServerId?: string;
  choose?: boolean;
  connect?: () => void;
}

/**
 * Inspect's first step when members sign each person in with their own
 * account: connect them, then see the tools. Shown in place of the tool list,
 * because a list missing those members' tools reads as the whole gateway.
 * Skipping shows what the viewer can already use.
 */
export function GatewayConnectStep({
  members,
  onSkip,
  availableCount,
  choosingConnection = false,
  connecting = false,
  connectionError = false,
}: {
  members: ConnectStepMember[];
  onSkip: () => void;
  /** Tools the viewer can use without connecting, for the skip link. */
  availableCount: number;
  choosingConnection?: boolean;
  connecting?: boolean;
  connectionError?: boolean;
}): JSX.Element {
  const names =
    members.length === 1
      ? members[0]!.name
      : `${members.length} servers on this gateway`;
  return (
    <div className="flex w-full max-w-2xl flex-col gap-6 px-6 pt-2 pb-10">
      <div className="space-y-2">
        <Heading variant="h4" className="normal-case">
          {choosingConnection ? "Choose connections" : "Connect your accounts"}
        </Heading>
        <Text muted small className="block">
          {choosingConnection
            ? `Review which credentials ${names} can use for this session. Then you can see and run the approved tools.`
            : `${names} ${members.length === 1 ? "signs" : "sign"} each person in with their own account. Connect to see and run ${members.length === 1 ? "its" : "their"} tools as you.`}
        </Text>
      </div>
      <ul className="bg-card divide-y border">
        {members.map((member) => (
          <li
            key={member.slug}
            className="flex items-center justify-between gap-4 px-4 py-3"
          >
            <span className="flex min-w-0 items-center gap-3">
              <SourceMcpIcon
                mcpServerId={member.mcpServerId}
                className="size-6 shrink-0 object-contain"
              />
              <span className="text-foreground truncate text-sm">
                {member.name}
              </span>
            </span>
            <Button
              variant="primary"
              size="sm"
              onClick={member.connect}
              disabled={(member.choose && connecting) || !member.connect}
              aria-label={
                member.choose
                  ? `Choose a connection for ${member.name}`
                  : `Connect your ${member.name} account`
              }
            >
              <Button.Text>
                {member.choose ? "Choose connection" : "Connect"}
              </Button.Text>
            </Button>
          </li>
        ))}
      </ul>
      {connectionError ? (
        <p role="alert">Couldn&apos;t start sign-in. Try again.</p>
      ) : null}
      <button
        type="button"
        onClick={onSkip}
        className="text-muted-foreground hover:text-foreground self-start text-sm underline underline-offset-2"
      >
        {availableCount > 0
          ? `Skip for now and show the ${availableCount} ${availableCount === 1 ? "tool" : "tools"} you can use`
          : "Skip for now"}
      </button>
    </div>
  );
}
