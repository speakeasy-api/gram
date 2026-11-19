import { Badge } from "@/components/ui/Badge";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { ShadowMCPReplacementOutcome } from "./useRemoteMcpInstallWorkflow";
import type {
  ShadowMCPReplacement,
  ShadowMCPReplacementState,
} from "./useShadowMCPReplacement";

/** The catalog add's question: replace the Shadow MCP version people use
 *  today with the server being added. On by default; one row per match. */
export function ShadowMCPReplacementOption({
  state,
}: {
  state: ShadowMCPReplacementState;
}): JSX.Element | null {
  if (state.candidates.length === 0) return null;
  return (
    <div className="space-y-2">
      {state.candidates.map((candidate) => (
        <ReplacementRow
          key={candidate.remoteUrl}
          candidate={candidate}
          checked={state.isSelected(candidate)}
          onCheckedChange={(checked) => state.setSelected(candidate, checked)}
        />
      ))}
    </div>
  );
}

function ReplacementRow({
  candidate,
  checked,
  onCheckedChange,
}: {
  candidate: ShadowMCPReplacement;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}): JSX.Element {
  const people =
    candidate.userCount === 1
      ? "1 person uses"
      : `${candidate.userCount} people use`;
  const usage =
    candidate.userCount > 0
      ? `${people} ${candidate.shadowName} directly today, as a Shadow MCP server.`
      : `${candidate.shadowName} is in use directly today, as a Shadow MCP server.`;
  return (
    <label
      className={cn(
        "flex cursor-pointer items-start gap-3 border p-3",
        checked ? "border-foreground" : "border-border",
      )}
    >
      <Checkbox
        aria-label={`Replace the Shadow MCP version of ${candidate.shadowName}`}
        checked={checked}
        onCheckedChange={(value) => onCheckedChange(value === true)}
        className="mt-0.5"
      />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium">
            Replace the Shadow MCP version
          </span>
          <Badge variant="success">Recommended</Badge>
        </div>
        <Text small muted>
          {usage} Speakeasy blocks direct use and sends agents to this server
          instead.
        </Text>
      </div>
    </label>
  );
}

/** What became of each Shadow MCP server the admin chose to replace. Every
 *  case that did not replace it names how to finish from the Shadow MCP
 *  page. */
export function ShadowMCPReplacementNotice({
  outcomes,
  projectSlug,
}: {
  outcomes: ShadowMCPReplacementOutcome[] | undefined;
  projectSlug?: string;
}): JSX.Element | null {
  const routes = useRoutes(projectSlug ? { projectSlug } : undefined);
  if (!outcomes || outcomes.length === 0) return null;
  return (
    <div className="space-y-2">
      {outcomes.map((outcome) => {
        const link = (
          <routes.shadowAI.mcps.detail.Link
            params={[outcome.shadowServerSlug]}
            className="text-foreground font-medium underline underline-offset-2"
          >
            {outcome.shadowName}
          </routes.shadowAI.mcps.detail.Link>
        );
        if (outcome.status === "replaced") {
          return (
            <div key={outcome.shadowServerSlug} className="border p-3">
              <Text small className="font-medium">
                Shadow MCP replaced
              </Text>
              <Text small muted>
                Direct use of {link} is blocked. Agents that try it are sent to{" "}
                {outcome.serverName}.
              </Text>
            </div>
          );
        }
        if (outcome.status === "setup-required") {
          return (
            <div key={outcome.shadowServerSlug} className="border p-3">
              <Text small className="font-medium">
                Shadow MCP not replaced yet
              </Text>
              <Text small muted>
                {outcome.serverName} needs setup before it can take over from{" "}
                {link}. Once it is enabled, link it from that page.
              </Text>
            </div>
          );
        }
        if (outcome.status === "not-added") {
          return (
            <div key={outcome.shadowServerSlug} className="border p-3">
              <Text small className="font-medium">
                Shadow MCP not replaced
              </Text>
              <Text small muted>
                The server was not added, so {link} still works directly.
              </Text>
            </div>
          );
        }
        return (
          <div
            key={outcome.shadowServerSlug}
            className="border-destructive/40 border p-3"
            role="alert"
          >
            <Text small className="text-destructive font-medium">
              Shadow MCP not replaced
            </Text>
            <Text small muted>
              {outcome.serverName} was added, but replacing {link} failed:{" "}
              {outcome.error}. Link it from that page.
            </Text>
          </div>
        );
      })}
    </div>
  );
}
