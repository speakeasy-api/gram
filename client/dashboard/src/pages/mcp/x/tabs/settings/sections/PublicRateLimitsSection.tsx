import { SharedTunnelConfirmDialog } from "@/components/mcp/shared-tunnel-impact";
import { Button } from "@/components/ui/Button";
import { Field, FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { RequireScope } from "@/components/require-scope";
import {
  formatTunneledMcpDisplay,
  getTunneledMcpServerArgs,
} from "@/lib/sources";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { STORED_VALUE_CHANGED_MESSAGE } from "./EditableSourceFieldSection";
import { MCP_PUBLIC_ACCESS_SECTION_ID } from "./PublicAccessSection";
import { invalidateTunneledMcpSourceViews } from "./sourceInvalidation";

// Moved here from the retired tunneled source detail page, which is where this
// section shipped: the tunnel has no page of its own now, so its public limit
// is set on the MCP server that fronts it.

type PublicRateLimitField = "publicRequestRatePerSecond" | "publicRequestBurst";

const PUBLIC_RATE_LIMIT_FIELDS: ReadonlyArray<{
  key: PublicRateLimitField;
  label: string;
  hint: string;
}> = [
  {
    key: "publicRequestRatePerSecond",
    label: "Requests per second",
    hint: "Sustained rate. Blank uses the default of 50/s.",
  },
  {
    key: "publicRequestBurst",
    label: "Burst",
    hint: "Requests admitted back-to-back from idle before the sustained rate applies. Blank means twice the rate (100 by default).",
  },
];

function formatPublicRate(server: TunneledMcpServer) {
  const stored = server.publicRequestRatePerSecond !== undefined;
  return `${server.effectivePublicRequestRatePerSecond}/s, burst ${server.effectivePublicRequestBurst}${stored ? "" : " (default)"}`;
}

type PublicRateDraft = Record<PublicRateLimitField, string>;

function sameRateDraft(a: PublicRateDraft, b: PublicRateDraft): boolean {
  return PUBLIC_RATE_LIMIT_FIELDS.every(({ key }) => a[key] === b[key]);
}

function toPublicRateDraft(server: TunneledMcpServer): PublicRateDraft {
  return Object.fromEntries(
    PUBLIC_RATE_LIMIT_FIELDS.map(({ key }) => [
      key,
      server[key] === undefined ? "" : String(server[key]),
    ]),
  ) as PublicRateDraft;
}

// Anonymous admission limit for this source. One bucket per tunnel is shared
// by every anonymous caller, so it bounds the load reaching the upstream
// server rather than fairness between callers. Unset fields keep the
// deployment-wide defaults, which the API reports as the effective values.
export function PublicRateLimitsSection({
  tunneledMcpServerId,
  projectId,
  mcpServerId,
}: {
  tunneledMcpServerId: string;
  /** The MCP server whose settings page renders this section. */
  mcpServerId: string;
  /**
   * The server's own project, so the gate matches what saving will target.
   * Required: an omitted id would leave the write ungated by project.
   */
  projectId: string;
}): JSX.Element | null {
  const { data: tunneledMcpServer } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(tunneledMcpServerId),
  );
  if (!tunneledMcpServer) return null;
  return (
    <PublicRateLimits
      tunneledMcpServer={tunneledMcpServer}
      projectId={projectId}
      mcpServerId={mcpServerId}
    />
  );
}

type PublicRateLimitForm = Partial<Record<PublicRateLimitField, number>>;

function describeRateLimitForm(form: PublicRateLimitForm): string {
  return PUBLIC_RATE_LIMIT_FIELDS.filter(({ key }) => key in form)
    .map(({ key, label }) =>
      form[key] === 0 ? `${label}: default` : `${label}: ${form[key]}`,
    )
    .join(", ");
}

function PublicRateLimits({
  tunneledMcpServer,
  projectId,
  mcpServerId,
}: {
  projectId: string;
  mcpServerId: string;
  tunneledMcpServer: TunneledMcpServer;
}) {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState(() =>
    toPublicRateDraft(tunneledMcpServer),
  );
  const [error, setError] = useState<string>();
  // The change awaiting confirmation, frozen when Save was pressed, with the
  // stored values it was computed against.
  const [pending, setPending] = useState<{
    form: PublicRateLimitForm;
    base: PublicRateDraft;
  } | null>(null);
  // Spans the request and the refetch after it, which update.isPending does
  // not, so the confirmation cannot be resubmitted in between.
  const [applying, setApplying] = useState(false);
  // The stored values the draft was last synced from, so a refetch that
  // changes them only replaces an untouched draft and never clobbers edits.
  const syncedFrom = useRef(toPublicRateDraft(tunneledMcpServer));

  const { publicRequestRatePerSecond, publicRequestBurst } = tunneledMcpServer;
  useEffect(() => {
    const next = toPublicRateDraft(tunneledMcpServer);
    setDraft((current) => {
      const untouched = sameRateDraft(current, syncedFrom.current);
      syncedFrom.current = next;
      return untouched ? next : current;
    });
    // Resync only when a stored value changes, not on every refetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [publicRequestRatePerSecond, publicRequestBurst]);

  // Omitted fields leave the stored value alone; a cleared field sends 0,
  // which the server treats as "back to the deployment default".
  const changes = () => {
    const form: Partial<Record<PublicRateLimitField, number>> = {};
    for (const { key, label } of PUBLIC_RATE_LIMIT_FIELDS) {
      const text = draft[key].trim();
      const storedValue = tunneledMcpServer[key];
      if (text === "") {
        if (storedValue !== undefined) form[key] = 0;
        continue;
      }
      const value = Number(text);
      if (!Number.isInteger(value) || value < 1) {
        throw new Error(`${label} must be a whole number of at least 1`);
      }
      if (value !== storedValue) form[key] = value;
    }
    return form;
  };

  let dirty = false;
  try {
    dirty = Object.keys(changes()).length > 0;
  } catch {
    dirty = true;
  }

  const requestSave = () => {
    setError(undefined);
    let form: PublicRateLimitForm;
    try {
      form = changes();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Invalid value";
      setError(message);
      return;
    }
    if (Object.keys(form).length === 0) return;
    setPending({ form, base: toPublicRateDraft(tunneledMcpServer) });
  };

  const handleSave = async ({ form, base }: NonNullable<typeof pending>) => {
    setError(undefined);
    // The limit changed underneath the confirmation, e.g. from another tab:
    // the frozen change was computed against values no longer stored.
    const current = toPublicRateDraft(tunneledMcpServer);
    if (!sameRateDraft(current, base)) {
      setPending(null);
      setError(STORED_VALUE_CHANGED_MESSAGE);
      return;
    }
    setApplying(true);
    try {
      await update.mutateAsync({
        request: {
          updateTunneledMcpServerForm: {
            id: tunneledMcpServer.id,
            ...form,
          },
        },
      });
      await invalidateTunneledMcpSourceViews(queryClient);
      toast.success("Anonymous rate limit updated");
      setPending(null);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to update rate limits";
      setError(message);
      toast.error(message);
    } finally {
      setApplying(false);
    }
  };

  return (
    <div className="border p-6">
      <Text
        variant="subheading"
        id="tunneled-mcp-public-rate-limits-label"
        className="mb-1"
      >
        Anonymous Rate Limit
      </Text>
      <Text muted small className="mb-4 max-w-3xl">
        Caps how many requests anonymous callers can send to MCP servers
        fronting this source, across every MCP method. One token bucket is
        shared by all callers, so it bounds the total load on your server rather
        than fairness between callers. Requests over the limit get HTTP 429 with
        a Retry-After header.
        {tunneledMcpServer.allowPublic ? null : (
          <>
            {" "}
            Applies once{" "}
            <Link
              to={`#${MCP_PUBLIC_ACCESS_SECTION_ID}`}
              className="underline underline-offset-2"
            >
              Public Access
            </Link>{" "}
            is enabled.
          </>
        )}
      </Text>

      <dl className="mb-4 grid max-w-xl grid-cols-[auto_1fr] gap-x-6 gap-y-1">
        <dt>
          <Text muted small>
            Effective limit
          </Text>
        </dt>
        <dd>
          <Text small data-testid="public-rate-limit-requests">
            {formatPublicRate(tunneledMcpServer)}
          </Text>
        </dd>
      </dl>

      <RequireScope
        scope="mcp:write"
        resourceId={projectId}
        projectId={projectId}
        level="component"
      >
        <Stack gap={3}>
          <div className="grid max-w-xl grid-cols-1 gap-3 sm:grid-cols-2">
            {PUBLIC_RATE_LIMIT_FIELDS.map(({ key, label, hint }) => (
              <Field key={key}>
                <Text
                  small
                  className="mb-1"
                  id={`public-rate-limit-${key}-label`}
                >
                  {label}
                </Text>
                <Input
                  id={`public-rate-limit-${key}`}
                  aria-labelledby={`public-rate-limit-${key}-label`}
                  type="number"
                  inputMode="numeric"
                  min={1}
                  value={draft[key]}
                  onChange={(value) =>
                    setDraft((current) => ({ ...current, [key]: value }))
                  }
                  placeholder={String(
                    key === "publicRequestRatePerSecond"
                      ? tunneledMcpServer.effectivePublicRequestRatePerSecond
                      : tunneledMcpServer.effectivePublicRequestBurst,
                  )}
                  disabled={applying}
                />
                <Text muted small>
                  {hint}
                </Text>
              </Field>
            ))}
          </div>
          {error !== undefined && <FieldError>{error}</FieldError>}
          <Stack direction="horizontal" gap={2}>
            <Button
              variant="primary"
              disabled={!dirty || applying}
              onClick={requestSave}
            >
              {applying ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>{applying ? "Saving" : "Save limit"}</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </RequireScope>
      <SharedTunnelConfirmDialog
        open={pending !== null}
        onOpenChange={(open) => {
          if (!open) setPending(null);
        }}
        tunneledMcpServerId={tunneledMcpServer.id}
        tunnelName={formatTunneledMcpDisplay(tunneledMcpServer)}
        currentMcpServerId={mcpServerId}
        publicWarning={tunneledMcpServer.allowPublic}
        title="Change the anonymous rate limit?"
        description="The limit is one budget for the whole tunnel, shared by anonymous callers of every public MCP server on it."
        effect="Raising it lets more anonymous load reach the upstream server; lowering it throttles every public server on the tunnel."
        confirmLabel="Save limit"
        pendingLabel="Saving"
        isPending={applying}
        errorMessage={pending ? error : undefined}
        onConfirm={() => {
          if (pending) void handleSave(pending);
        }}
      >
        {pending ? (
          <Text small>New limit: {describeRateLimitForm(pending.form)}</Text>
        ) : null}
      </SharedTunnelConfirmDialog>
    </div>
  );
}
