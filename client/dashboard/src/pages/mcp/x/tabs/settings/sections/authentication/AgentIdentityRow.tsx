import { Badge } from "@/components/ui/Badge";
import { Input } from "@/components/ui/Input";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import { Eye, EyeOff } from "lucide-react";
import type { AgentCredentialFields } from "./useAgentCredentialDraft";

/**
 * The Agent Identity credential form: one format toggle, the fields that
 * format needs, and a preview of the exact header the upstream receives.
 */
export function AgentIdentityRow({
  draft,
  disabled,
  upstreamName,
}: {
  draft: AgentCredentialFields;
  disabled: boolean;
  upstreamName: string;
}): JSX.Element {
  return (
    <div className="space-y-3">
      <SegmentedControl
        value={draft.format}
        // Client Credentials is advertised but not built yet, so the segment
        // shows the roadmap without becoming a selectable dead end.
        onChange={(next) => {
          if (next !== "client-credentials") draft.setFormat(next);
        }}
        disabled={disabled}
        options={[
          { value: "bearer" as const, label: "Bearer" },
          { value: "basic" as const, label: "Basic" },
          { value: "manual" as const, label: "Manual" },
          {
            value: "client-credentials" as const,
            label: (
              <span className="flex items-center gap-2 opacity-50">
                Client credentials
                <Badge variant="neutral" size="sm">
                  <Badge.Text>Coming soon</Badge.Text>
                </Badge>
              </span>
            ),
          },
        ]}
      />

      {draft.format === "bearer" ? (
        <div className="grid gap-3 sm:grid-cols-[minmax(0,140px)_1fr]">
          <div className="space-y-1">
            <Text muted small className="block">
              Prefix
            </Text>
            <Input
              value={draft.prefix}
              onChange={draft.setPrefix}
              placeholder="Bearer"
              disabled={disabled}
              aria-label="Prefix"
            />
          </div>
          <div className="space-y-1">
            <Text muted small className="block">
              Token
            </Text>
            <Input
              value={draft.token}
              onChange={draft.setToken}
              reveal
              placeholder="Paste the token"
              disabled={disabled}
              aria-label="Token"
            />
          </div>
        </div>
      ) : null}

      {draft.format === "basic" ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <Text muted small className="block">
              Username
            </Text>
            <Input
              value={draft.username}
              onChange={draft.setUsername}
              placeholder="service-account"
              disabled={disabled}
              aria-label="Username"
            />
          </div>
          <div className="space-y-1">
            <Text muted small className="block">
              Password
            </Text>
            <Input
              value={draft.password}
              onChange={draft.setPassword}
              reveal
              disabled={disabled}
              aria-label="Password"
            />
          </div>
        </div>
      ) : null}

      {draft.format === "manual" ? (
        <div className="space-y-1">
          <Text muted small className="block">
            Header value
          </Text>
          <Input
            value={draft.manualValue}
            onChange={draft.setManualValue}
            reveal
            placeholder="Token abc123"
            disabled={disabled}
            aria-label="Header value"
          />
        </div>
      ) : null}

      <div className="space-y-1">
        <div className="flex items-center justify-between gap-2">
          <Text muted small>
            Sent to {upstreamName} as
          </Text>
          <button
            type="button"
            onClick={draft.toggleReveal}
            aria-label={draft.reveal ? "Hide credential" : "Show credential"}
            className="text-muted-foreground hover:text-foreground"
          >
            {draft.reveal ? (
              <EyeOff aria-hidden="true" className="size-4" />
            ) : (
              <Eye aria-hidden="true" className="size-4" />
            )}
          </button>
        </div>
        <div
          className="bg-muted/40 flex min-h-[38px] items-center gap-3 border px-3 py-2 font-mono text-xs"
          role="status"
          aria-label="Authorization preview"
        >
          <span className="text-muted-foreground">Authorization:</span>
          <span className="break-all">{draft.preview}</span>
        </div>
      </div>
    </div>
  );
}
