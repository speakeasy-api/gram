import { ResourceAuditFeed } from "@/components/auditlogs/resource-audit-feed";
import {
  InfoField,
  InfoFieldGrid,
  InfoSection,
  InfoText,
} from "@/components/detail-fields";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useListJsonWebKeys } from "@gram/client/react-query/listJsonWebKeys";
import { useMemo } from "react";
import { ExternalKeyLink } from "../ExternalKeyLink";

export function OverviewTab({ set }: { set: JSONWebKeySet }): JSX.Element {
  // Key events name the key as their subject, so the set's history has to ask
  // for every key that was ever published into it, revoked ones included. The
  // feed waits for that list to settle either way: after a failed load it still
  // shows the set's own events rather than spinning forever.
  const { data: keysData, isPending: keysPending } = useListJsonWebKeys(
    { setId: set.id, includeRevoked: true },
    undefined,
    { throwOnError: false },
  );
  // The audit filter accepts at most 200 subjects. Keys come back newest
  // first, so a set with an implausible number of them keeps its newest
  // 199 in the feed rather than failing the request outright.
  const subjectIds = useMemo(
    () => [
      set.id,
      ...(keysData?.keys ?? []).slice(0, 199).map((key) => key.id),
    ],
    [set.id, keysData],
  );

  return (
    <div className="flex max-w-4xl flex-col gap-8">
      <InfoSection title="Key set">
        <InfoFieldGrid>
          <InfoField label="Name">
            <InfoText>{set.name}</InfoText>
          </InfoField>
          <InfoField label="Created">
            <InfoText>
              <HumanizeDateTime date={set.createdAt} />
            </InfoText>
          </InfoField>
          <InfoField label="Updated">
            <InfoText>
              <HumanizeDateTime date={set.updatedAt} />
            </InfoText>
          </InfoField>
        </InfoFieldGrid>
      </InfoSection>

      <InfoSection title="Encryption key">
        <Text small muted>
          New keys are published from this KMS key. Keys already published keep
          signing with the key they were minted from, even after the set is
          pointed at another one.
        </Text>
        <InfoField label="Publishing from">
          <ExternalKeyLink externalKeyId={set.externalKeyId} />
        </InfoField>
      </InfoSection>

      <InfoSection title="History">
        <Text small muted>
          Every change to this set and to the keys published in it, newest
          first.
        </Text>
        <ResourceAuditFeed
          subjectIds={subjectIds}
          enabled={!keysPending}
          emptyMessage="No activity recorded for this key set yet."
        />
      </InfoSection>
    </div>
  );
}
