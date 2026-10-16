import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import type { PresenceGroup } from "./model";

/**
 * Who is here right now. Presence is a lease an agent renews, so this answers
 * "is anything listening?" — not "is an agent running right now".
 */
export function PresenceRail({
  groups,
  sessionInstanceId,
  canParticipate,
  busy,
  onMessage,
  onConnectAgent,
  /** The panel version is titled by its own header. */
  titled = true,
  className,
}: {
  groups: PresenceGroup[];
  sessionInstanceId?: string;
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
        {groups.length === 0 ? (
          <p className="text-muted-foreground text-sm">
            Nobody is connected. Agents appear here while they hold a session.
          </p>
        ) : null}
        {groups.map((group) => (
          <section key={group.subjectId} className="mb-4">
            <h3 className="text-muted-foreground mb-1 truncate text-xs">
              {group.owner}
            </h3>
            <ul className="space-y-2">
              {group.entries.map((entry) => (
                <li key={entry.instance.id} className="group/entry flex gap-2">
                  <span
                    className="bg-success-default mt-1.5 size-1.5 shrink-0 rounded-full"
                    aria-hidden
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-1.5">
                      <span className="min-w-0 truncate text-sm">
                        {entry.name}
                      </span>
                      {entry.instance.id === sessionInstanceId ? (
                        <span className="text-muted-foreground shrink-0 text-xs">
                          you
                        </span>
                      ) : null}
                    </div>
                    {entry.instance.note ? (
                      <p className="text-muted-foreground text-xs break-words whitespace-pre-wrap">
                        {entry.instance.note}
                      </p>
                    ) : null}
                  </div>
                  {entry.instance.id === sessionInstanceId ||
                  !entry.isAgent ||
                  !canParticipate ? null : (
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
                  )}
                </li>
              ))}
            </ul>
          </section>
        ))}
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
