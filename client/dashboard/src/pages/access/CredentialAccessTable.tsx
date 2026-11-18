import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  CLIENT_TYPES,
  clientTypeHint,
  clientTypeLabel,
  serverLabel,
} from "@/lib/auth-clients";
import { cn } from "@/lib/utils";
import { X } from "lucide-react";
import { Fragment, useMemo, useState, type JSX, type ReactNode } from "react";

import {
  allowCredentialRow,
  blockCredentialRow,
  canSplitCredentialRow,
  credentialChildren,
  type CredentialRef,
  credentialRowKey,
  credentialRowState,
  type CredentialRow,
  type CredentialRule,
  type CredentialHolder,
  credentialHolderCopy,
  credentialRuleKey,
  describeCredentialRule,
  otherCredentialRules,
  revokeCredentialRow,
  splitCredentialRow,
  unblockCredentialRow,
} from "./credentialAccessModel";
import { Cell, EditorSection, RowToggle } from "./roleMatrixParts";

// Eyebrow carries its own colour; a text colour beside it would merge it away.
const TH = "text-eyebrow px-2 py-3 text-center";
const TD = "px-2 py-2 text-center align-middle";
/** Name, use, used on, the row menu. */
const COLUMN_COUNT = 4;

const PERSONAL_TYPES = new Set(["remote_session", "ema"]);

function rowName(
  row: CredentialRow,
  clients: Map<string, CredentialRef>,
): string {
  switch (row.kind) {
    case "all":
      return "All credentials";
    case "type":
      return clientTypeLabel(row.clientType);
    case "client":
      return clients.get(row.clientId)?.name ?? "Removed credential";
  }
}

function countLabel(count: number): string {
  return count === 1 ? "1 credential" : `${count} credentials`;
}

/** Where a server-filtered view still has something to show. */
function serverHas(client: CredentialRef, serverId: string | undefined) {
  return !serverId || client.servers.some((server) => server.id === serverId);
}

/**
 * Which upstream credentials the holder may use, as a tree: every
 * credential, each kind, each credential. A tick on a wider row reaches every
 * credential under it, now and later; splitting it turns it into one tick per
 * row below, so one can be taken away while the rest stay.
 */
export function CredentialAccessTable({
  holder,
  rules,
  onChange,
  clients,
  clientsError,
  onRetry,
  projects,
  serverFilter,
  onClearServerFilter,
  disabled = false,
  sectionId,
  bare = false,
  renderOtherRule,
}: {
  holder: CredentialHolder;
  rules: CredentialRule[];
  onChange: (next: CredentialRule[]) => void;
  /** The organization's credentials, or null while they load. */
  clients: CredentialRef[] | null;
  clientsError: boolean;
  onRetry: () => void;
  projects: { id: string; name: string }[];
  /** Show only the credentials one server offers. */
  serverFilter?: { id: string; name: string } | null;
  onClearServerFilter?: () => void;
  disabled?: boolean;
  /** Anchors the section, so the server grid can scroll to it. */
  sectionId?: string;
  /** Leave out the section heading, for a page that already titles it. */
  bare?: boolean;
  /** Words for a rule no row shows, in place of the plain description. */
  renderOtherRule?: (rule: CredentialRule) => ReactNode;
}): JSX.Element {
  const copy = credentialHolderCopy(holder);
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set());
  const [pendingSplit, setPendingSplit] = useState<CredentialRow | null>(null);
  const list = useMemo(() => clients ?? [], [clients]);
  const byId = useMemo(
    () => new Map(list.map((client) => [client.id, client])),
    [list],
  );
  const projectNames = useMemo(
    () => new Map(projects.map((project) => [project.id, project.name])),
    [projects],
  );
  const filterId = serverFilter?.id;

  const typeRows = CLIENT_TYPES.filter(
    (clientType) =>
      !filterId ||
      list.some(
        (client) =>
          client.clientType === clientType && serverHas(client, filterId),
      ),
  );
  const clientsOf = (clientType: string) =>
    list
      .filter(
        (client) =>
          client.clientType === clientType && serverHas(client, filterId),
      )
      .sort((a, b) => a.name.localeCompare(b.name));

  const others = otherCredentialRules(rules, clients);

  const toggle = (row: CredentialRow) => {
    const { state } = credentialRowState(rules, row);
    switch (state) {
      case "off":
        onChange(allowCredentialRow(rules, row));
        return;
      case "on":
      case "inherited":
        onChange(revokeCredentialRow(rules, row, list));
        return;
      case "blocked":
        onChange(unblockCredentialRow(rules, row));
        return;
      case "inherited-blocked":
        return;
    }
  };

  const actions = (row: CredentialRow): Action[] => {
    if (disabled) return [];
    const { state } = credentialRowState(rules, row);
    const result: Action[] = [];
    if (canSplitCredentialRow(rules, row, list)) {
      result.push({
        label:
          row.kind === "all" ? "Split into kinds" : "Split into credentials",
        description: "Set each one on its own",
        onClick: () => setPendingSplit(row),
      });
    }
    if (state === "blocked") {
      result.push({
        label: "Remove block",
        onClick: () => onChange(unblockCredentialRow(rules, row)),
      });
    } else if (state !== "inherited-blocked") {
      result.push({
        label: "Block",
        description: "Wins over every other rule",
        destructive: true,
        separatorBefore: result.length > 0,
        onClick: () => onChange(blockCredentialRow(rules, row)),
      });
    }
    return result;
  };

  const reason = (row: CredentialRow): string | undefined => {
    const { state, from } = credentialRowState(rules, row);
    if (!from) return undefined;
    const name = rowName(from, byId);
    return state === "inherited-blocked"
      ? `Blocked by ${name}`
      : `Comes from ${name}. Untick to take only this one away.`;
  };

  const renderRow = (row: CredentialRow, depth: number, detail?: string) => {
    const key = credentialRowKey(row);
    const name = rowName(row, byId);
    const { state } = credentialRowState(rules, row);
    const client = row.kind === "client" ? byId.get(row.clientId) : undefined;
    const servers = client?.servers ?? [];
    const rowActions = actions(row);
    const expandable = row.kind === "type";
    const expanded = !!filterId || open.has(key);
    const personal = row.kind === "type" && PERSONAL_TYPES.has(row.clientType);
    return (
      <tr key={key} className={cn(depth === 0 && "bg-muted/30")}>
        <th
          scope="row"
          className={cn(
            "py-2 pr-2 text-left font-normal",
            depth === 2 ? "pl-10" : "pl-4",
          )}
        >
          {expandable ? (
            <RowToggle
              name={name}
              detail={detail}
              strong
              open={expanded}
              disabled={!!filterId}
              onToggle={() =>
                setOpen((prev) => {
                  const next = new Set(prev);
                  if (next.has(key)) next.delete(key);
                  else next.add(key);
                  return next;
                })
              }
            />
          ) : (
            <>
              <div
                className={cn(
                  "truncate",
                  depth === 0 && "font-medium",
                  client && !client.enabled && "text-muted-foreground",
                )}
              >
                {name}
                {client && !client.enabled && " · turned off"}
              </div>
              {detail && (
                <div className="text-muted-foreground text-xs">{detail}</div>
              )}
            </>
          )}
          {personal && (
            <SimpleTooltip tooltip={copy.personal}>
              <span
                tabIndex={0}
                className="text-muted-foreground ml-5 cursor-default text-xs underline decoration-dotted"
              >
                Own connection only
              </span>
            </SimpleTooltip>
          )}
        </th>
        <td className={TD}>
          <Cell
            state={state}
            label={`Use ${name}`}
            reason={reason(row)}
            narrowable={state === "inherited"}
            onToggle={disabled ? undefined : () => toggle(row)}
          />
        </td>
        <td className="truncate py-2 pr-2 pl-2 text-left">
          {row.kind === "client" ? (
            servers.length === 0 ? (
              <span className="text-muted-foreground">No servers</span>
            ) : (
              <SimpleTooltip
                tooltip={servers
                  .map((server) => serverLabel(server))
                  .join(", ")}
              >
                <span tabIndex={0} className="cursor-default">
                  {serverLabel(servers[0]!)}
                  {servers.length > 1 && (
                    <span className="text-muted-foreground">
                      {" "}
                      +{servers.length - 1}
                    </span>
                  )}
                </span>
              </SimpleTooltip>
            )
          ) : null}
        </td>
        <td className={TD}>
          {rowActions.length > 0 && (
            <MoreActions
              actions={rowActions}
              triggerAriaLabel={`More for ${name}`}
            />
          )}
        </td>
      </tr>
    );
  };

  const allState = credentialRowState(rules, { kind: "all" }).state;
  const splitChildren = pendingSplit
    ? credentialChildren(pendingSplit, list)
    : [];

  const body = (
    <>
      <div
        id={sectionId}
        className={cn("bg-card scroll-mt-24", !bare && "border-border border")}
      >
        {serverFilter && (
          <div className="border-border flex items-center justify-between gap-3 border-b px-4 py-2">
            <Text small>
              Credentials{" "}
              <span className="font-medium">{serverFilter.name}</span> offers
            </Text>
            <Button variant="tertiary" size="xs" onClick={onClearServerFilter}>
              <Button.LeftIcon>
                <X className="h-3.5 w-3.5" />
              </Button.LeftIcon>
              <Button.Text>Show all</Button.Text>
            </Button>
          </div>
        )}
        {clientsError && (
          <Alert variant="error" className="m-4 text-sm">
            <span className="flex flex-wrap items-center gap-2">
              Could not load this organization&rsquo;s credentials. Kinds of
              credential can still be set.
              <Button variant="tertiary" size="xs" onClick={onRetry}>
                <Button.Text>Retry</Button.Text>
              </Button>
            </span>
          </Alert>
        )}
        <div className="overflow-x-auto">
          <table className="w-full min-w-[36rem] table-fixed border-collapse text-sm">
            <colgroup>
              <col className="w-[45%]" />
              <col className="w-[12%]" />
              <col />
              <col className="w-12" />
            </colgroup>
            <thead>
              <tr className="border-border bg-muted/40 border-b">
                <th scope="col" className={cn(TH, "pl-4 text-left")}>
                  Credentials
                </th>
                <th scope="col" className={TH}>
                  Use
                </th>
                <th scope="col" className={cn(TH, "text-left")}>
                  Used on
                </th>
                <th scope="col" className={TH}>
                  <span className="sr-only">More</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-border divide-y">
              {renderRow(
                { kind: "all" },
                0,
                allState === "on"
                  ? "Including credentials added later"
                  : "Every kind, now and later",
              )}
              {typeRows.map((clientType) => {
                const row: CredentialRow = { kind: "type", clientType };
                const children = clientsOf(clientType);
                const expanded = !!filterId || open.has(credentialRowKey(row));
                return (
                  <Fragment key={clientType}>
                    {renderRow(
                      row,
                      1,
                      clients === null
                        ? clientTypeHint(clientType)
                        : countLabel(children.length),
                    )}
                    {expanded &&
                      children.map((client) =>
                        renderRow(
                          {
                            kind: "client",
                            clientId: client.id,
                            clientType: client.clientType,
                          },
                          2,
                        ),
                      )}
                    {expanded && clients !== null && children.length === 0 && (
                      <tr>
                        <td
                          colSpan={COLUMN_COUNT}
                          className="text-muted-foreground py-2 pl-10 text-sm"
                        >
                          None set up yet. A tick on the kind covers ones added
                          later.
                        </td>
                      </tr>
                    )}
                  </Fragment>
                );
              })}
              {clients === null && !clientsError && (
                <tr>
                  <td colSpan={COLUMN_COUNT} className="p-4">
                    <Skeleton>
                      <div className="h-8 w-full" />
                    </Skeleton>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {others.length > 0 && (
          <div className="border-border w-full border-t">
            <div className="border-border border-b px-4 py-3">
              <div className="text-eyebrow text-muted-foreground">
                Other rules
              </div>
              <Text muted small>
                Narrower than a row above, or for a credential that no longer
                exists. They stay as they are when you save.
              </Text>
            </div>
            <ul className="divide-border divide-y">
              {others.map((rule) => {
                const described = describeCredentialRule(
                  rule,
                  byId,
                  projectNames,
                );
                const text = `${rule.scope === "mcp:blocked_auth" ? "Blocks" : "Allows"} ${
                  rule.selector.resourceId === "*"
                    ? described.charAt(0).toLowerCase() + described.slice(1)
                    : described
                }`;
                return (
                  <li
                    key={credentialRuleKey(rule)}
                    className="flex items-center justify-between gap-3 px-4 py-2"
                  >
                    {renderOtherRule ? (
                      <span className="flex flex-wrap items-baseline gap-1 text-sm">
                        {rule.scope === "mcp:blocked_auth"
                          ? "Blocks"
                          : "Allows"}{" "}
                        {renderOtherRule(rule)}
                      </span>
                    ) : (
                      <span className="text-sm">{text}</span>
                    )}
                    {!disabled && (
                      <Button
                        variant="tertiary"
                        size="sm"
                        onClick={() =>
                          onChange(
                            rules.filter(
                              (r) =>
                                credentialRuleKey(r) !==
                                credentialRuleKey(rule),
                            ),
                          )
                        }
                        aria-label={`Remove rule: ${text}`}
                      >
                        <Button.LeftIcon>
                          <X className="h-4 w-4" />
                        </Button.LeftIcon>
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </div>
      {!bare && (
        <Text muted small>
          A block wins over every other rule. A person never uses another
          person&rsquo;s connection, whatever the rules say.
        </Text>
      )}
      <ConfirmDialog
        open={!!pendingSplit}
        onOpenChange={(next) => {
          if (!next) setPendingSplit(null);
        }}
        title={`Split ${pendingSplit ? rowName(pendingSplit, byId) : ""}?`}
        description={
          pendingSplit?.kind === "all"
            ? `This one rule becomes one per kind of credential (${splitChildren.length}). Nobody loses access now, but a new kind of credential is no longer covered.`
            : `This one rule becomes one per credential (${splitChildren.length}). Nobody loses access now, but credentials of this kind added later are no longer covered.`
        }
        confirmLabel="Split"
        confirmVariant="primary"
        onConfirm={() => {
          if (pendingSplit) {
            onChange(splitCredentialRow(rules, pendingSplit, list));
            if (pendingSplit.kind === "all") {
              setOpen(new Set());
            } else {
              setOpen((prev) =>
                new Set(prev).add(credentialRowKey(pendingSplit)),
              );
            }
          }
          setPendingSplit(null);
        }}
      />
    </>
  );
  if (bare) return body;
  return (
    <EditorSection title="Upstream credentials" description={copy.description}>
      {body}
    </EditorSection>
  );
}
