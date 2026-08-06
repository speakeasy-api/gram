import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { UnknownSkillActivation } from "@gram/client/models/components/unknownskillactivation.js";
import { useUnknownSkillActivationsInfinite } from "@gram/client/react-query/unknownSkillActivations.js";
import { Badge } from "@/components/ui/Badge";
import { type Column, Table } from "@/components/ui/Table";
import { useState } from "react";

const reasonLabels: Record<string, string> = {
  invalid_name: "Invalid name",
  unresolved_hash: "Manifest not captured",
  ambiguous_hash: "Ambiguous version",
};

export function UnknownSkillActivationsSection(): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  const query = useUnknownSkillActivationsInfinite({ limit: 50 }, undefined, {
    throwOnError: false,
  });
  const activations =
    query.data?.pages.flatMap((page) => page.result.activations) ?? [];

  if (query.isPending && !query.data) return null;
  if (!query.error && activations.length === 0) return null;

  if (!expanded && activations.length > 0) {
    return (
      <section
        className="space-y-3 pt-6"
        aria-labelledby="unknown-skills-title"
      >
        <UnknownActivationsHeading />
        <Button variant="secondary" onClick={() => setExpanded(true)}>
          View unknown activations
        </Button>
      </section>
    );
  }
  const columns: Column<UnknownSkillActivation>[] = [
    {
      key: "skill",
      header: "Reported skill",
      render: (activation) => (
        <Text small mono>
          {activation.skillName}
        </Text>
      ),
    },
    { key: "provider", header: "Provider", render: (row) => row.provider },
    {
      key: "source",
      header: "Source",
      render: (row) => row.source || "Not reported",
    },
    {
      key: "reason",
      header: "Reason",
      render: (row) => (
        <Badge variant="neutral">
          <Badge.Text>{reasonLabels[row.reason] ?? row.reason}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "seen",
      header: "Activated",
      width: "150px",
      render: (row) => (
        <Text small muted title={dateTimeFormatters.full.format(row.seenAt)}>
          <HumanizeDateTime date={row.seenAt} />
        </Text>
      ),
    },
  ];

  return (
    <section className="space-y-3 pt-6" aria-labelledby="unknown-skills-title">
      <UnknownActivationsHeading />
      {query.error && !query.data ? (
        <div className="space-y-3">
          <ErrorAlert
            title="Unable to load unknown activations"
            error={query.error}
          />
          <Button variant="secondary" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </div>
      ) : (
        <Table
          columns={columns}
          data={activations}
          rowKey={(row) => row.id}
          noResultsMessage={<Text>No unknown activations found.</Text>}
        />
      )}
      {query.isFetchNextPageError && (
        <ErrorAlert
          title="Unable to load more unknown activations"
          error={query.error ?? "Try again."}
        />
      )}
      {query.hasNextPage && (
        <Button
          variant="secondary"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {query.isFetchingNextPage
            ? "Loading..."
            : query.isFetchNextPageError
              ? "Retry loading activations"
              : "Load more activations"}
        </Button>
      )}
    </section>
  );
}

function UnknownActivationsHeading(): JSX.Element {
  return (
    <div>
      <Text id="unknown-skills-title" variant="subheading" as="h3">
        Unknown activations
      </Text>
      <Text small muted>
        Activations whose manifest could not be matched to one skill version.
      </Text>
    </div>
  );
}
