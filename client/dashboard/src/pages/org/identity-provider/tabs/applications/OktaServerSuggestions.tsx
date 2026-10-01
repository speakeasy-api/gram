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
  installed: { label: "Installed", variant: "information" },
};

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
        <ul className="grid grid-cols-1 gap-4 lg:grid-cols-2">
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
  return (
    <Card className="h-full">
      <Card.Header>
        <Card.Title>{suggestion.title ?? suggestion.serverName}</Card.Title>
        <Card.Description>{suggestion.description}</Card.Description>
        <Card.Info>
          <Badge variant={badge.variant} size="sm">
            {badge.label}
          </Badge>
        </Card.Info>
      </Card.Header>
      <Card.Content>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <span className="text-eyebrow">In your Okta</span>
            <ul className="flex flex-col gap-1">
              {suggestion.oktaApplications.map((app) => (
                <li
                  key={app.oktaAppId}
                  className="flex flex-wrap items-center gap-2"
                >
                  <Text small>{app.label}</Text>
                  <Badge variant="neutral" size="sm">
                    {humanizeOktaToken(app.signOnMode)}
                  </Badge>
                  {!app.xaaSupported && (
                    <Text muted small>
                      This sign-on mode cannot be used for Cross App Access.
                    </Text>
                  )}
                </li>
              ))}
            </ul>
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-eyebrow">
              {suggestion.remotes.length > 1 ? "Endpoints" : "Endpoint"}
            </span>
            {suggestion.remotes.map((remote) => (
              <code key={remote.url} className="font-mono text-xs break-all">
                {remote.url}
              </code>
            ))}
          </div>
          {suggestion.state === "open" &&
            !isSuggestionInstallable(suggestion) && (
              <Text muted small>
                None of these endpoints use streamable HTTP over HTTPS, so this
                server cannot be added from here yet.
              </Text>
            )}
          {suggestion.state === "dismissed" && suggestion.dismissedAt && (
            <Text muted small>
              Dismissed <HumanizeDateTime date={suggestion.dismissedAt} />.
            </Text>
          )}
          {suggestion.state === "installed" && (
            <Text muted small>
              A server in this organization already uses this endpoint.
            </Text>
          )}
        </div>
      </Card.Content>
      <Card.Footer>
        <Card.Actions>
          {suggestion.state === "open" && (
            <>
              <Button size="sm" disabled={busy || !canAdd} onClick={onAdd}>
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
            <Button
              variant="secondary"
              size="sm"
              disabled={busy}
              onClick={onRestore}
            >
              Restore
            </Button>
          )}
          {suggestion.documentationUrl && (
            <Button variant="tertiary" size="sm" asChild>
              <a
                href={suggestion.documentationUrl}
                target="_blank"
                rel="noopener noreferrer"
              >
                Documentation
              </a>
            </Button>
          )}
        </Card.Actions>
      </Card.Footer>
    </Card>
  );
}
