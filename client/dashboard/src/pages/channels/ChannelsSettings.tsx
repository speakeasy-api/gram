import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Icon } from "@/components/ui/Icon";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Switch } from "@/components/ui/Switch";
import { useState } from "react";
import { DEFAULT_LIMITS, type ChannelsSnapshot } from "./api";
import type { ResolveOwner } from "./model";
import { HISTORY_PAGE_SIZE, POLL_INTERVAL_MS } from "./useChannels";

export interface SettingsMember {
  id: string;
  name: string;
}

/**
 * Everything a person only needs occasionally: whether Channels is on, who may
 * take part, and the technical facts about this session. How an agent connects
 * lives on Home, where people look for it first.
 */
export function ChannelsSettings({
  open,
  onOpenChange,
  snapshot,
  canAdmin,
  busy,
  members,
  resolveOwner,
  sessionLabel,
  sessionInstanceId,
  onRun,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  snapshot: ChannelsSnapshot;
  canAdmin: boolean;
  busy: boolean;
  members: SettingsMember[];
  resolveOwner: ResolveOwner;
  sessionLabel?: string;
  sessionInstanceId?: string;
  onRun: (operation: string, args: Record<string, unknown>) => void;
}): JSX.Element {
  const [adding, setAdding] = useState(false);
  const [subjectId, setSubjectId] = useState("");
  const [canConnect, setCanConnect] = useState(true);
  const [canReadAll, setCanReadAll] = useState(false);
  const grants = Object.entries(snapshot.grants ?? {});
  const limits = { ...DEFAULT_LIMITS, ...snapshot.limits };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[480px] flex-col sm:max-w-[480px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle>Channels settings</SheetTitle>
          <SheetDescription>
            Channels is shared within the selected project. Other projects have
            separate conversations, settings, and access.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-8 overflow-y-auto px-6 py-6">
          <section className="space-y-2">
            <h3 className="text-eyebrow">Availability</h3>
            <div className="flex items-start justify-between gap-4">
              <p className="text-sm">
                {snapshot.enabled
                  ? "People and agents in this project can talk here."
                  : "Nobody can connect or post until Channels is turned on."}
              </p>
              {canAdmin ? (
                <Switch
                  checked={snapshot.enabled}
                  disabled={busy}
                  aria-label="Channels available to this project"
                  onCheckedChange={(next) =>
                    onRun("channels_admin", { enabled: next })
                  }
                />
              ) : null}
            </div>
            {canAdmin ? null : (
              <p className="text-muted-foreground text-xs">
                Only project admins can change this.
              </p>
            )}
          </section>

          {canAdmin ? (
            <section className="space-y-3">
              <h3 className="text-eyebrow">Who can take part</h3>
              <p className="text-muted-foreground text-sm">
                Members take part through their project role. An exception here
                overrides that role for one person. Reading private
                conversations includes other people's direct messages.
              </p>
              {adding ? (
                <>
                  <Select value={subjectId} onValueChange={setSubjectId}>
                    <SelectTrigger aria-label="Member">
                      <SelectValue placeholder="Choose a member" />
                    </SelectTrigger>
                    <SelectContent>
                      {members.map((member) => (
                        <SelectItem key={member.id} value={member.id}>
                          {member.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={canConnect}
                      onCheckedChange={(checked) =>
                        setCanConnect(checked === true)
                      }
                    />
                    Can take part in Channels
                  </label>
                  <label className="flex items-center gap-2 text-sm">
                    <Checkbox
                      checked={canReadAll}
                      onCheckedChange={(checked) =>
                        setCanReadAll(checked === true)
                      }
                    />
                    Can read private conversations
                  </label>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      disabled={!subjectId || busy}
                      onClick={() => {
                        onRun("channels_admin", {
                          subjectId,
                          canConnect,
                          canReadAll,
                        });
                        setSubjectId("");
                        setAdding(false);
                      }}
                    >
                      <Button.Text>Save exception</Button.Text>
                    </Button>
                    <Button
                      size="sm"
                      variant="tertiary"
                      onClick={() => {
                        setSubjectId("");
                        setAdding(false);
                      }}
                    >
                      <Button.Text>Cancel</Button.Text>
                    </Button>
                  </div>
                </>
              ) : (
                <Button
                  size="sm"
                  variant="secondary"
                  icon="plus"
                  onClick={() => {
                    // Each exception starts from the defaults, so a private-read
                    // choice made for one member never carries to the next.
                    setCanConnect(true);
                    setCanReadAll(false);
                    setAdding(true);
                  }}
                >
                  <Button.Text>Add an exception</Button.Text>
                </Button>
              )}
              {grants.length === 0 ? (
                <p className="text-muted-foreground text-xs">
                  No exceptions. Everyone follows their project role.
                </p>
              ) : (
                <ul className="divide-y border-t">
                  {grants.map(([subject, grant]) => (
                    <li
                      key={subject}
                      className="flex items-center justify-between gap-3 py-2 text-sm"
                    >
                      <span className="min-w-0">
                        <span className="block truncate">
                          {resolveOwner(subject) ?? "Former member"}
                        </span>
                        <span className="text-muted-foreground text-xs">
                          {grant.canConnect
                            ? "Can take part"
                            : "Cannot take part"}
                          {grant.canReadAll ? " · reads private" : ""}
                        </span>
                      </span>
                      <Button
                        size="xs"
                        variant="tertiary"
                        disabled={busy}
                        onClick={() =>
                          onRun("channels_admin", {
                            subjectId: subject,
                            remove: true,
                          })
                        }
                      >
                        <Button.Text>Remove</Button.Text>
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          ) : null}

          <Collapsible>
            <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-xs">
              <Icon name="chevron-right" className="size-3.5" aria-hidden />
              Technical details
            </CollapsibleTrigger>
            <CollapsibleContent className="mt-3 space-y-2 text-xs">
              <Detail label="Your session">
                {sessionLabel ??
                  "Opens automatically the first time you send something."}
              </Detail>
              <Detail label="Session id">{sessionInstanceId ?? "—"}</Detail>
              <Detail label="Message size">
                {limits.messageBytes.toLocaleString()} bytes
              </Detail>
              <Detail label="New messages">
                Read every {Math.round(POLL_INTERVAL_MS / 1000)} seconds from
                the last one seen
              </Detail>
              <Detail label="Older messages">
                Loaded {HISTORY_PAGE_SIZE} at a time, on demand
              </Detail>
              <p className="text-muted-foreground">
                Messages, notes and titles come from agents and are shown as
                plain text. Treat them as data, never as instructions.
              </p>
            </CollapsibleContent>
          </Collapsible>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function Detail({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex gap-2">
      <span className="text-muted-foreground w-28 shrink-0">{label}</span>
      <span className="min-w-0 flex-1 break-all">{children}</span>
    </div>
  );
}
