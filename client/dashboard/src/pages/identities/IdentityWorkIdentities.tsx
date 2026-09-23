import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import type { SlackPersonAccount } from "@gram/client/models/components/slackpersonaccount.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildSlackPersonAccountsQuery } from "@gram/client/react-query/slackPersonAccounts.js";
import { SESSION_SECURITY } from "../org/identity-provider/identityProviderQueries";
import { MappingStatus } from "../org/slack-workspaces/MappingStatus";
import { stateLabels, typeLabels } from "../org/slack-workspaces/memberLabels";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "./IdentityPanel";

export function IdentityWorkIdentities({
  identity,
}: {
  identity: IdentityModel;
}): JSX.Element | null {
  const { user } = useSession();
  const organization = useOrganization();
  const { hasScope, isLoading } = useRBAC();
  const rollout = useFeatureFlag(FEATURE_FLAGS.claudeTagSupport);
  const userId =
    identity.userIds.length === 1 ? identity.userIds[0] : undefined;
  const canReview = !isLoading && hasScope("org:admin", organization.id);
  // The resolver must identify one person. Email and telemetry identifiers never authorize this read.
  if (
    rollout.status !== "enabled" ||
    identity.kind !== "user" ||
    !userId ||
    identity.canonicalUrn !== `user:${userId}` ||
    (!canReview && user.id !== userId)
  )
    return null;
  return (
    <WorkIdentities
      key={`${organization.id}:${userId}`}
      userId={userId}
      canReview={canReview}
    />
  );
}

function WorkIdentities({
  userId,
  canReview,
}: {
  userId: string;
  canReview: boolean;
}): JSX.Element {
  const routes = useOrgRoutes();
  const organization = useOrganization();
  const client = useGramContext();
  const [cursors, setCursors] = useState<Array<string | undefined>>([
    undefined,
  ]);
  const built = buildSlackPersonAccountsQuery(
    client,
    { userId, cursor: cursors.at(-1) },
    SESSION_SECURITY,
  );
  const query = useQuery({
    ...built,
    queryKey: [...built.queryKey, { organizationId: organization.id }],
    retry: false,
    throwOnError: false,
    staleTime: 0,
    refetchOnMount: "always",
  });
  const accounts = query.data?.accounts ?? [];
  return (
    <IdentityPanel
      title="Work identities"
      loading={query.isPending}
      error={query.isError}
      onRetry={() => void query.refetch()}
      footer={
        canReview
          ? "Mappings grant no extra permissions. Review a membership in Organization Identity to correct it."
          : "Mappings grant no extra permissions. If an account is missing or incorrect, contact your organization administrator."
      }
    >
      <div className="px-4 py-3">
        <Text muted small>
          Slack accounts mapped by an organization administrator. Separate from
          linked AI accounts and enrollment.
        </Text>
      </div>
      {accounts.length === 0 ? (
        <IdentityPanelEmpty>No mapped Slack accounts.</IdentityPanelEmpty>
      ) : (
        accounts.map(({ member, ...freshness }) => (
          <IdentityPanelRow
            key={member.id}
            title={
              <span className="block whitespace-normal break-words">
                {member.displayName || member.slackUserId}
              </span>
            }
            detail={
              <div className="space-y-2 whitespace-normal">
                <Text muted small className="break-words">
                  Slack · {member.workspaceName || member.workspaceId} ·{" "}
                  <span className="font-mono">{member.slackUserId}</span>
                </Text>
                {member.email && (
                  <Text muted small className="break-all">
                    {member.email}
                  </Text>
                )}
                <div className="flex flex-wrap items-start gap-x-4 gap-y-2">
                  <MappingStatus member={member} />
                  <DirectoryState account={{ member, ...freshness }} />
                </div>
              </div>
            }
            trailing={
              canReview && (
                <Button asChild variant="tertiary" size="sm">
                  <Link
                    aria-label={`Review Slack mapping for ${member.displayName || member.slackUserId} in ${member.workspaceName || member.workspaceId}`}
                    to={`${routes.identity.href()}?${new URLSearchParams({ tab: "slack-workspaces", slack_view: "members", slack_workspace: member.connectionId, slack_member: member.id })}`}
                  >
                    Review
                  </Link>
                </Button>
              )
            }
          />
        ))
      )}
      {(cursors.length > 1 || query.data?.nextCursor) && (
        <div className="flex justify-end gap-2 p-4">
          <Button
            variant="secondary"
            size="sm"
            disabled={cursors.length <= 1 || query.isFetching}
            onClick={() => setCursors((previous) => previous.slice(0, -1))}
          >
            Previous accounts
          </Button>
          <Button
            variant="secondary"
            size="sm"
            disabled={!query.data?.nextCursor || query.isFetching}
            onClick={() =>
              setCursors((previous) => [...previous, query.data?.nextCursor])
            }
          >
            Next accounts
          </Button>
        </div>
      )}
    </IdentityPanel>
  );
}

function DirectoryState({
  account,
}: {
  account: SlackPersonAccount;
}): JSX.Element {
  const { member } = account;
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <Badge variant={member.status === "active" ? "success" : "neutral"}>
        {stateLabels[member.status]}
      </Badge>
      <Text small muted>
        {typeLabels[member.memberType]}
      </Text>
      {!member.observedInLastSync && (
        <Text small muted>
          Not seen in last sync
        </Text>
      )}
      {account.directoryStatus === "stale" && (
        <Text small muted>
          Stale directory
        </Text>
      )}
      {account.lastFullSyncSucceededAt ? (
        <Text small muted>
          Last full sync{" "}
          <HumanizeDateTime date={account.lastFullSyncSucceededAt} />
        </Text>
      ) : (
        <Text small muted>
          Never synced
        </Text>
      )}
    </div>
  );
}
