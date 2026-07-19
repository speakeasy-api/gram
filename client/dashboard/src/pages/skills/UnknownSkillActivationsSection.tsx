import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import type { UnknownSkillActivation } from "@gram/client/models/components/unknownskillactivation.js";
import { useUnknownSkillActivationsInfinite } from "@gram/client/react-query/unknownSkillActivations.js";
import { Badge, type Column, Table } from "@speakeasy-api/moonshine";
import { useState } from "react";

const reasonLabels: Record<string, string> = {
  invalid_name: "Invalid name",
  unresolved_hash: "Manifest not captured",
  ambiguous_hash: "Ambiguous version",
};

export function UnknownSkillActivationsSection(): JSX.Element | null {
  const [enabled, setEnabled] = useState(false);
  const query = useUnknownSkillActivationsInfinite({ limit: 50 }, undefined, {
    enabled,
    throwOnError: false,
  });
  const activations =
    query.data?.pages.flatMap((page) => page.result.activations) ?? [];

  if (!enabled) {
    return (
      <section
        className="space-y-3 pt-6"
        aria-labelledby="unknown-skills-title"
      >
        <UnknownActivationsHeading />
        <Button variant="outline" onClick={() => setEnabled(true)}>
          View unknown activations
        </Button>
      </section>
    );
  }
  if (query.isPending && !query.data) {
    return (
      <section
        className="space-y-3 pt-6"
        aria-label="Loading unknown activations"
      >
        <UnknownActivationsHeading />
        <SkeletonTable />
      </section>
    );
  }
  if (!query.error && query.data && activations.length === 0) return null;

  const columns: Column<UnknownSkillActivation>[] = [
    {
      key: "skill",
      header: "Reported skill",
      render: (activation) => (
        <Type small mono>
          {activation.skillName}
        </Type>
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
        <Type small muted title={dateTimeFormatters.full.format(row.seenAt)}>
          <HumanizeDateTime date={row.seenAt} />
        </Type>
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
          <Button variant="outline" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </div>
      ) : (
        <Table columns={columns} data={activations} rowKey={(row) => row.id} />
      )}
      {query.isFetchNextPageError && (
        <ErrorAlert
          title="Unable to load more unknown activations"
          error={query.error ?? "Try again."}
        />
      )}
      {query.hasNextPage && (
        <Button
          variant="outline"
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
      <Type id="unknown-skills-title" variant="subheading" as="h3">
        Unknown activations
      </Type>
      <Type small muted>
        Activations whose manifest could not be matched to one skill version.
      </Type>
    </div>
  );
}
