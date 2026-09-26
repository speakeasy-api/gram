import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";
import {
  REDACTED_SECRET,
  type HeaderDraft,
  type HeaderDraftError,
  type HeaderDraftsState,
  type HeaderSource,
  type IdentityMode,
} from "@/lib/remote-identity";
import { Badge } from "@/components/ui/Badge";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Lock, Plus, Trash2 } from "lucide-react";
import { Link } from "react-router";

/** What the lock on a row the identity section owns says when you ask it. */
const MANAGED_BY_USER = "Managed by User Identity";
const DISABLED_BY_USER = "Disabled by User Identity";
const MANAGED_BY_AGENT = "Managed by Agent Identity";

/**
 * A row that exists on the server under the current mode. Under User Identity
 * that is a leftover static credential, which is ignored rather than used —
 * the row stays visible so it can be cleaned up.
 */
function savedManagedLabel(mode: IdentityMode | undefined): string {
  return mode === "user" ? DISABLED_BY_USER : MANAGED_BY_AGENT;
}

/**
 * The Authorization row User Identity stands in for. It is never saved, never
 * validated and never written: it exists so the name reads as spoken for.
 */
const RESERVED_AUTHORIZATION: HeaderDraft = {
  key: "reserved-authorization",
  name: "Authorization",
  source: "static",
  staticValue: "",
  valueFromRequestHeader: "",
  isRequired: true,
  isSecret: false,
  hadSecret: false,
};

function emptyHeadersMessage(reserved: boolean): string {
  if (reserved) return "No other upstream headers configured yet.";
  return "No upstream headers configured yet.";
}

function noop(): void {}

/**
 * One template for the header labels and every row beneath them. The columns
 * are fixed rather than shared out, so a row without the Required/Secret flags
 * — one the identity section owns — still lines its fields up with the rest.
 */
/**
 * A field the operator has to come back to. Warning rather than destructive:
 * nothing is broken, the row just is not finished, and the same orange carries
 * the message in the footer.
 */
const WARN_BORDER = "border-warning-default";

const ROW_GRID =
  "grid grid-cols-[minmax(0,1fr)_11rem_minmax(0,2fr)_11rem_3rem] items-center gap-x-3";

/**
 * The upstream header rows.
 *
 * Presentational: the draft state, the validation and the write all belong to
 * `useHeaderDrafts`, because the identity panel's footer commits these rows
 * alongside the identity itself and needs to know their state to do it.
 */
export function HeadersSection({
  state,
  resourceId,
  siblingMcpServers,
}: {
  state: HeaderDraftsState;
  resourceId?: string;
  /**
   * Other MCP servers backed by the same remote source. Their headers are the
   * same rows, and since #6524 removed the remote's own page this is the only
   * place to edit them — so the change is named rather than locked.
   */
  siblingMcpServers: readonly McpServer[];
}): JSX.Element {
  const { authorization, readOnly } = state;
  const routes = useRoutes();
  // User Identity fills Authorization from whoever is signed in, so there is
  // nothing to save and nothing to see — which reads, in a list of rows, as
  // the name simply being free. Show it as taken instead, rather than letting
  // someone type it and collect a validation error for their trouble.
  const reserved =
    authorization.mode === "user" && !authorization.managedHeaderId;

  return (
    <Stack gap={4}>
      {siblingMcpServers.length > 0 ? (
        <Alert variant="warning" dismissible={false}>
          <Stack gap={1}>
            <Text small>
              These headers are stored on the remote source, which also backs{" "}
              {siblingMcpServers.length}{" "}
              {siblingMcpServers.length === 1
                ? "other MCP server"
                : "other MCP servers"}
              . Changes here apply to every one of them:
            </Text>
            <div className="flex flex-wrap gap-2">
              {siblingMcpServers.map((server) => (
                <Link
                  key={server.id}
                  to={routes.mcp.x.settings.href(mcpServerRouteParam(server))}
                  className="no-underline"
                >
                  <Badge variant="neutral" className="hover:bg-muted">
                    <Badge.Text>{server.name || "MCP Server"}</Badge.Text>
                  </Badge>
                </Link>
              ))}
            </div>
          </Stack>
        </Alert>
      ) : null}

      {authorization.unknown ? (
        <Alert variant="error" dismissible={false}>
          Could not determine the current identity configuration. Header editing
          is disabled.
        </Alert>
      ) : null}

      {state.isLoading ? (
        <Text muted small>
          Loading headers…
        </Text>
      ) : (
        <Stack gap={4}>
          {state.drafts.some((draft) => draft.fromCatalog) ? (
            <Text muted small>
              These headers are suggested from this endpoint's MCP catalog
              entry. Fill in the values and save, or remove the ones you don't
              need.
            </Text>
          ) : null}

          {reserved || state.drafts.length > 0 ? (
            // One bordered list rather than a card per row: at a row apiece the
            // borders were most of what the eye had to get through.
            <div className="divide-y border">
              <div className={cn(ROW_GRID, "bg-muted/30 px-3 py-2")}>
                <span className="text-eyebrow">Header name</span>
                <span className="text-eyebrow">Value source</span>
                <span className="text-eyebrow">Value</span>
                <span />
                <span />
              </div>

              {reserved ? (
                <HeaderDraftRow
                  draft={RESERVED_AUTHORIZATION}
                  readOnly
                  managed={MANAGED_BY_USER}
                  valuePlaceholder="Filled by the identity provider"
                  legacyPassThroughAuthorization={false}
                  resourceId={resourceId}
                  error={null}
                  onChange={noop}
                  onRemove={noop}
                />
              ) : null}

              {state.drafts.map((draft, index) => {
                const managed =
                  !!draft.id && draft.id === authorization.managedHeaderId;
                return (
                  <HeaderDraftRow
                    key={draft.key}
                    draft={draft}
                    readOnly={readOnly || managed}
                    managed={
                      managed ? savedManagedLabel(authorization.mode) : null
                    }
                    legacyPassThroughAuthorization={
                      !!draft.id &&
                      draft.id === authorization.passThroughHeaderId
                    }
                    resourceId={resourceId}
                    error={
                      state.reportErrors
                        ? (state.fieldErrors.get(draft.key) ?? null)
                        : null
                    }
                    onChange={(next) => state.replaceHeader(index, next)}
                    onRemove={() => state.removeHeader(index)}
                  />
                );
              })}
            </div>
          ) : null}

          {state.drafts.length === 0 ? (
            <Text muted small>
              {emptyHeadersMessage(reserved)}
            </Text>
          ) : null}
        </Stack>
      )}

      {readOnly ? null : (
        <>
          {state.error ? (
            <Alert variant="error" dismissible={false}>
              {state.error.message}
            </Alert>
          ) : null}

          <RequireScope
            scope="mcp:write"
            resourceId={resourceId}
            level="component"
          >
            <Button
              variant="secondary"
              size="md"
              disabled={state.isLoading || authorization.unknown}
              onClick={state.addHeader}
            >
              <Button.LeftIcon>
                <Plus className="size-4" />
              </Button.LeftIcon>
              <Button.Text>Add header</Button.Text>
            </Button>
          </RequireScope>
        </>
      )}
    </Stack>
  );
}

function HeaderDraftRow({
  draft,
  readOnly,
  managed,
  legacyPassThroughAuthorization,
  resourceId,
  valuePlaceholder = "Bearer …",
  error,
  onChange,
  onRemove,
}: {
  draft: HeaderDraft;
  readOnly: boolean;
  /** Set when the identity section owns this row, with what the lock says. */
  managed: string | null;
  legacyPassThroughAuthorization: boolean;
  resourceId?: string;
  valuePlaceholder?: string;
  /** The row's problem, when there is one worth pointing at yet. */
  error: HeaderDraftError | null;
  onChange: (draft: HeaderDraft) => void;
  onRemove: () => void;
}): JSX.Element {
  // A saved secret arrives as its redacted placeholder. There is nothing to
  // reveal until the operator replaces it, so the field is plain text.
  const reveal = draft.isSecret && draft.staticValue !== REDACTED_SECRET;

  return (
    <div className="p-3">
      <Stack gap={2}>
        {legacyPassThroughAuthorization ? (
          <Alert variant="warning" dismissible={false}>
            Legacy pass-through Authorization. Remove this row before using
            Agent Identity or relying on No Identity.
          </Alert>
        ) : null}

        <div className={ROW_GRID} data-slot="header-row">
          <Input
            value={draft.name}
            disabled={readOnly}
            onChange={(value) => onChange({ ...draft, name: value })}
            placeholder="Header name"
            aria-label="Header name"
            className={error?.field === "name" ? WARN_BORDER : undefined}
          />

          <Select
            value={draft.source}
            disabled={readOnly}
            onValueChange={(value) => {
              const source = value as HeaderSource;
              onChange({
                ...draft,
                source,
                isSecret: source === "static" ? draft.isSecret : false,
              });
            }}
          >
            {/* The trigger is w-fit by default; the grid track sets the width
              here, so it has to fill it or the column reads ragged. */}
            <SelectTrigger className="w-full" aria-label="Value source">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="static">Static value</SelectItem>
              <SelectItem value="request">From request header</SelectItem>
            </SelectContent>
          </Select>

          <div className="min-w-0">
            {draft.source === "static" ? (
              <Input
                value={draft.staticValue}
                disabled={readOnly}
                onChange={(value) => onChange({ ...draft, staticValue: value })}
                onFocus={(event) => {
                  // Editing a redacted secret should replace it, not append to
                  // the `***` placeholder. Select it so the first keystroke
                  // wins.
                  if (draft.staticValue === REDACTED_SECRET) {
                    event.currentTarget.select();
                  }
                }}
                placeholder={valuePlaceholder}
                aria-label="Header value"
                reveal={reveal}
                className={error?.field === "value" ? WARN_BORDER : undefined}
              />
            ) : null}
            {draft.source === "request" ? (
              <Input
                value={draft.valueFromRequestHeader}
                disabled={readOnly}
                onChange={(value) =>
                  onChange({ ...draft, valueFromRequestHeader: value })
                }
                placeholder="X-Forwarded-Authorization"
                aria-label="Inbound request header"
                className={error?.field === "value" ? WARN_BORDER : undefined}
              />
            ) : null}
          </div>

          {/* Both trailing columns are always rendered, empty where they do not
            apply: grid places children in order, so a skipped one would pull
            the next across and break the alignment this template exists for. */}
          {managed ? (
            <span />
          ) : (
            <div className="flex items-center gap-3 justify-self-start">
              <label className="flex items-center gap-1.5">
                <Checkbox
                  checked={draft.isRequired}
                  disabled={readOnly}
                  onCheckedChange={(checked) =>
                    onChange({ ...draft, isRequired: checked === true })
                  }
                />
                <Text muted small>
                  Required
                </Text>
              </label>
              {draft.source === "static" ? (
                <label className="flex items-center gap-1.5">
                  <Checkbox
                    checked={draft.isSecret}
                    disabled={readOnly}
                    onCheckedChange={(checked) =>
                      onChange({ ...draft, isSecret: checked === true })
                    }
                  />
                  <Text muted small>
                    Secret
                  </Text>
                </label>
              ) : null}
            </div>
          )}

          <ManagedOrRemove
            managed={managed}
            readOnly={readOnly}
            name={draft.name}
            resourceId={resourceId}
            onRemove={onRemove}
          />
        </div>
      </Stack>
    </div>
  );
}

/**
 * The row's last column: a lock when the identity section owns the row, the
 * delete button when it does not, and an empty cell when neither applies. It
 * is one component so all three occupy the same box and the icon lands in the
 * same place down the list.
 */
function ManagedOrRemove({
  managed,
  readOnly,
  name,
  resourceId,
  onRemove,
}: {
  managed: string | null;
  readOnly: boolean;
  name: string;
  resourceId?: string;
  onRemove: () => void;
}): JSX.Element {
  if (managed) {
    return (
      <SimpleTooltip tooltip={managed}>
        <span
          role="img"
          aria-label={managed}
          className="flex h-9 w-12 items-center justify-center text-muted-foreground"
        >
          <Lock aria-hidden="true" className="size-4" />
        </span>
      </SimpleTooltip>
    );
  }

  if (readOnly) return <span />;

  return (
    <RequireScope scope="mcp:write" resourceId={resourceId} level="component">
      <Button
        variant="tertiary"
        size="md"
        onClick={onRemove}
        aria-label={`Remove header ${name || "row"}`}
      >
        <Button.LeftIcon>
          <Trash2 className="size-4" />
        </Button.LeftIcon>
      </Button>
    </RequireScope>
  );
}
