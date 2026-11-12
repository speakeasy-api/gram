import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import type { Presence } from "./model";

/**
 * Who is here right now. Presence is a lease an agent renews, so this answers
 * "is anything listening?" — not "is an agent running right now". Agents are
 * listed by the name they chose; who connected them is secondary.
 */
export function PresenceRail({
  presence,
  canParticipate,
  busy,
  onMessage,
  onConnectAgent,
  /** The panel version is titled by its own header. */
  titled = true,
  className,
}: {
  presence: Presence;
  canParticipate: boolean;
  busy: boolean;
  onMessage: (instanceId: string) => void;
  onConnectAgent: () => void;
  titled?: boolean;
  className?: string;
}): JSX.Element {
  return (
    <div className={cn("flex min-h-0 min-w-0 flex-col", className)}>
      {titled ? (
        <h2 className="text-eyebrow px-4 pt-4 pb-2">Online now</h2>
      ) : null}
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
        {presence.agents.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            No agents are connected. They show up here, under the name they
            choose, while they are connected.
          </p>
        ) : (
          <ul className="space-y-3">
            {presence.agents.map((entry) => (
              <li key={entry.instance.id} className="group/entry flex gap-2">
                <span
                  className="bg-success-default mt-1.5 size-1.5 shrink-0 rounded-full"
                  aria-hidden
                />
                <div className="min-w-0 flex-1">
                  <span className="block truncate text-sm">{entry.name}</span>
                  {entry.instance.note ? (
                    <p className="text-muted-foreground text-xs break-words whitespace-pre-wrap">
                      {entry.instance.note}
                    </p>
                  ) : null}
                  <p className="text-muted-foreground/70 truncate text-xs">
                    {entry.isYours ? "Yours" : entry.owner}
                  </p>
                </div>
                {canParticipate ? (
                  <Button
                    size="xs"
                    variant="tertiary"
                    icon="at-sign"
                    aria-label={`Message ${entry.name}`}
                    tooltip={`Message ${entry.name} privately`}
                    disabled={busy}
                    onClick={() => onMessage(entry.instance.id)}
                    className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover/entry:opacity-100 [@media(hover:none)]:opacity-100"
                  />
                ) : null}
              </li>
            ))}
          </ul>
        )}
        {presence.readers.length > 0 ? (
          <p className="text-muted-foreground mt-4 text-xs">
            Reading along: {presence.readers.join(", ")}
          </p>
        ) : null}
      </div>
      <div className="border-t px-4 py-3">
        <button
          type="button"
          onClick={onConnectAgent}
          className="text-muted-foreground hover:text-foreground flex items-center gap-2 text-xs"
        >
          <Icon name="plug" className="size-3.5" aria-hidden />
          Connect an agent
        </button>
      </div>
    </div>
  );
}
