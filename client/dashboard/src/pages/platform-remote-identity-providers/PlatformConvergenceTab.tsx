import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import type { IssuerConvergenceCandidate } from "@gram/client/models/components/issuerconvergencecandidate.js";
import { useGlobalRemoteSessionIssuerConvergenceCandidatesInfinite } from "@gram/client/react-query/globalRemoteSessionIssuerConvergenceCandidates.js";
import { useState } from "react";
import { ScopeBadge } from "../remote-identity-providers/ScopeBadge";
import { issuerDisplayName } from "../remote-identity-providers/issuerDisplay";
import { MigrateToGlobalIssuerDialog } from "./MigrateToGlobalIssuerDialog";
import {
  candidateBlockerSummary,
  candidateIsBlocked,
  candidateOwnerLabel,
} from "./convergenceBlockers";

// PlatformConvergenceTab lists the organization- and project-level providers
// that name the same upstream as this platform provider, so a platform admin can
// fold them onto the shared catalog entry.
//
// Candidates whose metadata differs are deliberately still listed, with the
// differing fields named. Filtering them out would leave an admin staring at an
// empty tab with no way to learn that a near-miss exists or what to fix.
export function PlatformConvergenceTab({
  issuer,
}: {
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  const [selected, setSelected] = useState<IssuerConvergenceCandidate | null>(
    null,
  );

  // Paged rather than single-shot: the case this tab exists for is a widely
  // adopted upstream, which is exactly when candidates overflow one page. A
  // truncated list would also make the empty-state copy below a false claim.
  const {
    data,
    isLoading,
    isError,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
  } = useGlobalRemoteSessionIssuerConvergenceCandidatesInfinite({
    targetId: issuer.id,
  });

  const columns: Column<IssuerConvergenceCandidate>[] = [
    {
      key: "provider",
      header: "Provider",
      render: (candidate) => (
        <Stack gap={1}>
          <Text className="font-medium">
            {issuerDisplayName(candidate.issuer)}
          </Text>
          <ScopeBadge
            projectId={candidate.issuer.projectId}
            organizationId={candidate.issuer.organizationId}
          />
        </Stack>
      ),
    },
    {
      key: "organization",
      header: "Organization",
      render: (candidate) => <Text>{candidateOwnerLabel(candidate)}</Text>,
    },
    {
      key: "clients",
      header: "Clients",
      width: "100px",
      render: (candidate) => <Text>{candidate.clientCount}</Text>,
    },
    {
      key: "status",
      header: "Status",
      render: (candidate) => (
        <Text small muted={!candidateIsBlocked(candidate)}>
          {candidateBlockerSummary(candidate)}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "150px",
      render: (candidate) => (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setSelected(candidate)}
          disabled={candidateIsBlocked(candidate)}
        >
          <Button.Text>Consolidate</Button.Text>
        </Button>
      ),
    },
  ];

  const candidates =
    data?.pages.flatMap((page) => page.result.items ?? []) ?? [];

  // Only a failure with nothing to show replaces the tab. Once a page has
  // loaded, a later page failing must not discard the organizations already on
  // screen: those rows are still accurate, and dropping them would look like the
  // candidates disappeared rather than like one request failed.
  if (isError && candidates.length === 0) {
    return <Text muted>Could not load convergence candidates.</Text>;
  }

  if (isLoading) {
    return <Text muted>Loading…</Text>;
  }

  return (
    <Stack gap={4}>
      <Text small muted>
        Organizations with their own provider for {issuer.issuer}. Consolidating
        one moves its clients onto this platform provider and removes the
        original. Existing sessions keep working, so nobody signs in again.
      </Text>

      <Table
        columns={columns}
        data={candidates}
        rowKey={(candidate) => candidate.issuer.id}
        noResultsMessage={
          <Text>
            No organization has its own provider for this upstream identity
            provider.
          </Text>
        }
      />

      {isError && (
        <Text small muted>
          Could not load the remaining organizations. The rows above are
          complete; retry to load the rest.
        </Text>
      )}

      {hasNextPage && (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void fetchNextPage()}
          disabled={isFetchingNextPage}
        >
          <Button.Text>
            {isFetchingNextPage ? "Loading…" : isError ? "Retry" : "Load more"}
          </Button.Text>
        </Button>
      )}

      {selected && (
        <MigrateToGlobalIssuerDialog
          candidate={selected}
          targetId={issuer.id}
          targetName={issuerDisplayName(issuer)}
          onClose={() => setSelected(null)}
        />
      )}
    </Stack>
  );
}
