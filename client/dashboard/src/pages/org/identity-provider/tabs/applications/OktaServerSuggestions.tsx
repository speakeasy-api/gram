import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SkeletonParagraph } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { useOrganization } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import type { OktaServerSuggestion } from "@gram/client/models/components/oktaserversuggestion.js";
import { useDismissOktaServerSuggestionMutation } from "@gram/client/react-query/dismissOktaServerSuggestion.js";
import {
  invalidateAllOktaServerSuggestions,
  useOktaServerSuggestions,
} from "@gram/client/react-query/oktaServerSuggestions.js";
import { useRestoreOktaServerSuggestionMutation } from "@gram/client/react-query/restoreOktaServerSuggestion.js";

import { SESSION_SECURITY } from "../../identityProviderQueries";
import { humanizeOktaToken } from "./applicationsView";
import {
  isSuggestionInstallable,
  suggestionToCatalogServer,
} from "./suggestionToCatalogServer";

type Filter = "open" | "all";

const STATE_BADGE: Record<
  OktaServerSuggestion["state"],
  { label: string; variant: "success" | "neutral" | "information" }
> = {
  open: { label: "Suggested", variant: "success" },
  dismissed: { label: "Dismissed", variant: "neutral" },
  installed: { label: "Installed", variant: "neutral" },
};

const DETAIL_LABEL =
  "flex h-6 items-center self-center border border-information-muted bg-information-softest px-2 text-xs whitespace-nowrap text-default-information";
const DETAIL_LINE = "flex h-6 min-w-0 items-center";

function inlineError(error: unknown): void {
  toast.error(error instanceof Error ? error.message : "Request failed");
}

/**
 * Catalog servers whose Okta mapping matches an app in this organization's
 * snapshot. Nothing is created until the admin adds one through the usual
 * remote MCP install flow; dismissals are remembered per organization.
 */
export function OktaServerSuggestions(): JSX.Element {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const [filter, setFilter] = useState<Filter>("open");
  const [projectSlug, setProjectSlug] = useState(
    organization.projects[0]?.slug ?? "",
  );
  const [adding, setAdding] = useState<OktaServerSuggestion | null>(null);
  const suggestions = useOktaServerSuggestions(
    { includeAll: filter === "all" },
    SESSION_SECURITY,
    {
      throwOnError: false,
      retry: false,
      placeholderData: (previous) => previous,
    },
  );
  const refresh = () => invalidateAllOktaServerSuggestions(queryClient);
  const dismiss = useDismissOktaServerSuggestionMutation({
    onSuccess: () => {
      toast.success("Suggestion dismissed.");
      void refresh();
    },
    onError: inlineError,
  });
  const restore = useRestoreOktaServerSuggestionMutation({
    onSuccess: () => {
      toast.success("Suggestion restored.");
      void refresh();
    },
    onError: inlineError,
  });
  const busy = dismiss.isPending || restore.isPending;
  const dismissEntry = (registryEntryId: string) =>
    dismiss.mutate({
      security: SESSION_SECURITY,
      request: {
        dismissOktaServerSuggestionRequestBody: { registryEntryId },
      },
    });
  const restoreEntry = (registryEntryId: string) =>
    restore.mutate({
      security: SESSION_SECURITY,
      request: {
        restoreOktaServerSuggestionRequestBody: { registryEntryId },
      },
    });

  const data = suggestions.data;
  return (
    <section
      aria-labelledby="okta-server-suggestions"
      className="flex min-w-0 flex-col gap-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <span id="okta-server-suggestions" className="text-eyebrow">
            Suggested MCP servers
          </span>
          <Text muted small className="max-w-2xl">
            Catalog servers for applications your organization has in Okta. Add
            one to create it with its endpoint filled in, or dismiss it;
            dismissals are remembered for the organization.
          </Text>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {organization.projects.length > 1 && (
            <Select value={projectSlug} onValueChange={setProjectSlug}>
              <SelectTrigger aria-label="Project to add servers to">
                <SelectValue placeholder="Project" />
              </SelectTrigger>
              <SelectContent>
                {organization.projects.map((project) => (
                  <SelectItem key={project.id} value={project.slug}>
                    {project.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <SegmentedControl<Filter>
            value={filter}
            onChange={setFilter}
            options={[
              { value: "open", label: "Suggested" },
              { value: "all", label: "All" },
            ]}
          />
        </div>
      </div>
      {suggestions.isError && <ApiErrorAlert error={suggestions.error} />}
      {suggestions.isPending && <SkeletonParagraph />}
      {data && data.suggestions.length === 0 && (
        <InlineEmptyState
          icon="layout-grid"
          orientation="horizontal"
          heading={
            filter === "open"
              ? "No new suggestions"
              : "No Okta applications map to catalog servers yet"
          }
          description={
            filter === "open"
              ? "Every application in your Okta snapshot that has a catalog server is added or dismissed. Switch to All to review them."
              : "Suggestions appear once an active, assigned application in your Okta snapshot matches a server in the catalog."
          }
        />
      )}
      {data && data.suggestions.length > 0 && (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(20rem,1fr))] gap-4">
          {data.suggestions.map((suggestion) => (
            <li key={suggestion.registryEntryId}>
              <SuggestionCard
                suggestion={suggestion}
                busy={busy}
                canAdd={
                  projectSlug !== "" && isSuggestionInstallable(suggestion)
                }
                onAdd={() => setAdding(suggestion)}
                onDismiss={() => dismissEntry(suggestion.registryEntryId)}
                onRestore={() => restoreEntry(suggestion.registryEntryId)}
              />
            </li>
          ))}
        </ul>
      )}
      {data?.snapshotAt && (
        <Text muted small>
          Based on the applications snapshot from{" "}
          <HumanizeDateTime date={data.snapshotAt} />.
        </Text>
      )}
      {adding && (
        <AddServerDialog
          servers={[suggestionToCatalogServer(adding)]}
          open
          onOpenChange={(open) => {
            if (!open) setAdding(null);
          }}
          projectSlug={projectSlug}
          onInstallFinished={() => void refresh()}
        />
      )}
    </section>
  );
}

function SuggestionCard({
  suggestion,
  busy,
  canAdd,
  onAdd,
  onDismiss,
  onRestore,
}: {
  suggestion: OktaServerSuggestion;
  busy: boolean;
  canAdd: boolean;
  onAdd: () => void;
  onDismiss: () => void;
  onRestore: () => void;
}): JSX.Element {
  const badge = STATE_BADGE[suggestion.state];
  const installable = isSuggestionInstallable(suggestion);
  const name = suggestion.title ?? suggestion.serverName;
  const apps = primaryFirst(
    suggestion.oktaApplications,
    (app) => app.xaaSupported,
  );
  const slugs = [...new Set(apps.map((app) => app.name))];
  const remotes = primaryFirst(suggestion.remotes, isInstallableRemote);
  return (
    <Card className="h-full">
      <div className="flex items-center gap-3">
        <SuggestionIcon name={name} iconUrl={suggestion.iconUrl} />
        <Card.Title className="min-w-0 flex-1 truncate">{name}</Card.Title>
        {suggestion.state !== "open" && (
          <Badge variant={badge.variant} size="sm" className="shrink-0">
            {badge.label}
          </Badge>
        )}
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3 gap-y-2 pt-1 text-sm">
        <dt className={DETAIL_LABEL}>
          {apps.length > 1 ? "Okta apps" : "Okta app"}
        </dt>
        <dd className="flex min-w-0 flex-col">
          {apps.map((app) => (
            <OktaApplicationLine key={app.oktaAppId} app={app} />
          ))}
        </dd>
        <dt className={DETAIL_LABEL}>
          {slugs.length > 1 ? "Okta slugs" : "Okta slug"}
        </dt>
        <dd className="flex min-w-0 flex-col">
          {slugs.map((slug) => (
            <code key={slug} className={`${DETAIL_LINE} font-mono text-xs`}>
              <span className="truncate" title={slug}>
                {slug}
              </span>
            </code>
          ))}
        </dd>
        <dt className={DETAIL_LABEL}>
          {remotes.length > 1 ? "Endpoints" : "Endpoint"}
        </dt>
        <dd className="flex min-w-0 flex-col">
          {remotes.map((remote) => (
            <code
              key={remote.url}
              className={`${DETAIL_LINE} font-mono text-xs`}
            >
              <span className="truncate" title={remote.url}>
                {remote.url}
              </span>
            </code>
          ))}
        </dd>
      </dl>
      {suggestion.state === "open" && !installable && (
        <Text muted small>
          None of these endpoints use streamable HTTP over HTTPS, so this server
          cannot be added from here yet.
        </Text>
      )}
      <Card.Footer className="mt-auto">
        <div className="flex min-h-8 flex-wrap items-center gap-3">
          {suggestion.state === "open" && (
            <>
              <Button
                size="sm"
                variant="success"
                disabled={busy || !canAdd || !installable}
                onClick={onAdd}
              >
                Add server
              </Button>
              <Button
                variant="secondary"
                size="sm"
                disabled={busy}
                onClick={onDismiss}
              >
                Dismiss
              </Button>
            </>
          )}
          {suggestion.state === "dismissed" && (
            <>
              <Button
                variant="secondary"
                size="sm"
                disabled={busy}
                onClick={onRestore}
              >
                Restore
              </Button>
              {suggestion.dismissedAt && (
                <Text muted small>
                  Dismissed <HumanizeDateTime date={suggestion.dismissedAt} />.
                </Text>
              )}
            </>
          )}
          {suggestion.state === "installed" && (
            <Text muted small>
              A server in this organization already uses this endpoint.
            </Text>
          )}
        </div>
      </Card.Footer>
    </Card>
  );
}

/** Vendor icon tile; falls back to the first letter when there is no icon or it fails to load. */
function SuggestionIcon({
  name,
  iconUrl,
}: {
  name: string;
  iconUrl: string | undefined;
}): JSX.Element {
  const [failed, setFailed] = useState(false);
  return (
    <span
      aria-hidden="true"
      className="flex size-8 shrink-0 items-center justify-center border bg-card text-sm font-semibold text-muted-foreground"
    >
      {iconUrl && !failed ? (
        <img
          src={iconUrl}
          alt=""
          className="size-5 object-contain"
          onError={() => setFailed(true)}
        />
      ) : (
        name.charAt(0).toUpperCase()
      )}
    </span>
  );
}

/** Stable reorder that leads with the entries the card should show first. */
function primaryFirst<T>(items: T[], isPrimary: (item: T) => boolean): T[] {
  return [...items.filter(isPrimary), ...items.filter((i) => !isPrimary(i))];
}

function isInstallableRemote(remote: { type: string; url: string }): boolean {
  return remote.type === "streamable-http" && remote.url.startsWith("https://");
}

type OktaApplication = OktaServerSuggestion["oktaApplications"][number];

function OktaApplicationLine({ app }: { app: OktaApplication }): JSX.Element {
  const mode = (
    <Badge variant="neutral" size="sm" className="shrink-0">
      {humanizeOktaToken(app.signOnMode)}
    </Badge>
  );
  return (
    <span className="flex min-h-6 min-w-0 flex-wrap items-center gap-x-2">
      <span className="max-w-full truncate">{app.label}</span>
      {app.xaaSupported ? (
        mode
      ) : (
        <SimpleTooltip tooltip="This sign-on mode cannot be used for Cross App Access.">
          <span className="flex shrink-0">{mode}</span>
        </SimpleTooltip>
      )}
    </span>
  );
}
