import { Button } from "@/components/ui/Button";
import { Field, FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { RequireScope } from "@/components/require-scope";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import {
  invalidateAllGetTunneledMcpServer,
  useGetTunneledMcpServer,
} from "@gram/client/react-query/getTunneledMcpServer.js";
import { invalidateAllTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

// Moved here from the retired tunneled source detail page, which is where this
// section shipped: the tunnel has no page of its own now, so its public limit
// is set on the MCP server that fronts it.
async function invalidateTunneledMcpServerViews(queryClient: QueryClient) {
  await Promise.all([
    invalidateAllGetTunneledMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
  ]);
}

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

function toPublicRateDraft(
  server: TunneledMcpServer,
): Record<PublicRateLimitField, string> {
  return Object.fromEntries(
    PUBLIC_RATE_LIMIT_FIELDS.map(({ key }) => [
      key,
      server[key] === undefined ? "" : String(server[key]),
    ]),
  ) as Record<PublicRateLimitField, string>;
}

// Anonymous admission limit for this source. One bucket per tunnel is shared
// by every anonymous caller, so it bounds the load reaching the upstream
// server rather than fairness between callers. Unset fields keep the
// deployment-wide defaults, which the API reports as the effective values.
export function PublicRateLimitsSection({
  tunneledMcpServerId,
  projectId,
}: {
  tunneledMcpServerId: string;
  /** The server's own project, so the gate matches what saving will target. */
  projectId?: string;
}): JSX.Element | null {
  const { data: tunneledMcpServer } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(tunneledMcpServerId),
  );
  if (!tunneledMcpServer) return null;
  return (
    <PublicRateLimits
      tunneledMcpServer={tunneledMcpServer}
      projectId={projectId}
    />
  );
}

function PublicRateLimits({
  tunneledMcpServer,
  projectId,
}: {
  projectId?: string;
  tunneledMcpServer: TunneledMcpServer;
}) {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState(() =>
    toPublicRateDraft(tunneledMcpServer),
  );
  const [error, setError] = useState<string>();
  // The stored values the draft was last synced from, so a refetch that
  // changes them only replaces an untouched draft and never clobbers edits.
  const syncedFrom = useRef(toPublicRateDraft(tunneledMcpServer));

  const { publicRequestRatePerSecond, publicRequestBurst } = tunneledMcpServer;
  useEffect(() => {
    const next = toPublicRateDraft(tunneledMcpServer);
    setDraft((current) => {
      const untouched = PUBLIC_RATE_LIMIT_FIELDS.every(
        ({ key }) => current[key] === syncedFrom.current[key],
      );
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

  const handleSave = async () => {
    setError(undefined);
    let form: Partial<Record<PublicRateLimitField, number>>;
    try {
      form = changes();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Invalid value";
      setError(message);
      return;
    }
    if (Object.keys(form).length === 0) return;
    try {
      await update.mutateAsync({
        request: {
          updateTunneledMcpServerForm: {
            id: tunneledMcpServer.id,
            ...form,
          },
        },
      });
      await invalidateTunneledMcpServerViews(queryClient);
      toast.success("Anonymous rate limit updated");
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to update rate limits";
      setError(message);
      toast.error(message);
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
        {tunneledMcpServer.allowPublic
          ? null
          : " Applies once Public Access is enabled."}
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

      <RequireScope scope="mcp:write" resourceId={projectId} level="component">
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
                  disabled={update.isPending}
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
              disabled={!dirty || update.isPending}
              onClick={() => void handleSave()}
            >
              {update.isPending ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {update.isPending ? "Saving" : "Save limit"}
              </Button.Text>
            </Button>
          </Stack>
        </Stack>
      </RequireScope>
    </div>
  );
}
