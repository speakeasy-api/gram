import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Spinner } from "@/components/ui/Spinner";
import { StatusDot } from "@/components/ui/StatusDot";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { McpFeedbackSettings } from "@gram/client/models/components/mcpfeedbacksettings.js";

/**
 * Whether agents can send feedback about this server, and the two things an
 * editor does about it: switch it, or change what agents see. The switch sits
 * in the same place in both states and applies at once.
 */
export function FeedbackToolPanel({
  settings,
  canEdit,
  saving,
  onToggle,
  onCustomize,
}: {
  settings: McpFeedbackSettings;
  /** Whether the viewer may change the server; readers see the state only. */
  canEdit: boolean;
  saving: boolean;
  onToggle: (enabled: boolean) => void;
  onCustomize: () => void;
}): JSX.Element {
  return (
    <div className="bg-card flex flex-wrap items-center gap-x-6 gap-y-4 border px-6 py-5">
      <div className="flex min-w-64 flex-1 flex-col gap-1">
        <Heading variant="h5" className="flex items-center gap-2 font-medium">
          <StatusDot tone={settings.enabled ? "success" : "neutral"} />
          {settings.enabled ? "Feedback is on" : "Feedback is off"}
        </Heading>
        <Text small muted>
          {settings.enabled ? (
            <>
              Agents see a tool named{" "}
              <span className="text-foreground font-mono break-all">
                {settings.tool.name}
              </span>{" "}
              on this server and call it to report what blocked or confused
              them.
            </>
          ) : (
            "Turn it on to hear from the agents using this server. It adds one tool that agents call to report what blocked or confused them, and their reports appear here. Detected secrets and personal data are masked before a report is stored, on a best-effort basis."
          )}
        </Text>
        {settings.updatedAt && (
          <Text small muted>
            {settings.updatedByEmail
              ? `Changed by ${settings.updatedByEmail} · `
              : "Changed "}
            <HumanizeDateTime date={settings.updatedAt} includeTime={false} />
          </Text>
        )}
      </div>
      {canEdit ? (
        <div className="flex shrink-0 items-center gap-2">
          <Button
            variant={settings.enabled ? "secondary" : "tertiary"}
            size="sm"
            disabled={saving}
            onClick={onCustomize}
          >
            <Button.Text>Customize</Button.Text>
          </Button>
          <Button
            variant={settings.enabled ? "tertiary" : "primary"}
            size="sm"
            disabled={saving}
            onClick={() => onToggle(!settings.enabled)}
          >
            {saving && (
              <Button.LeftIcon>
                <Spinner className="mr-0 size-3.5" />
              </Button.LeftIcon>
            )}
            <Button.Text>
              {settings.enabled ? "Turn off" : "Turn on feedback"}
            </Button.Text>
          </Button>
        </div>
      ) : (
        <Text small muted className="shrink-0">
          Only people who can edit this server can change this.
        </Text>
      )}
    </div>
  );
}
