import { InlineEmptyState } from "@/components/inline-empty-state";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListJsonWebKeySetsQuery } from "@gram/client/react-query/listJsonWebKeySets";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { ListSection } from "../ListSection";
import { CreateJsonWebKeySetSheet } from "./CreateJsonWebKeySetSheet";
import { ExternalKeyLink } from "./ExternalKeyLink";

const SECTION_DESCRIPTION =
  "JSON Web Key Sets (JWKS) publish the public half of your KMS keys so identity providers and MCP servers can verify the tokens Speakeasy signs.";

// SigningKeysSection is the JSON Web Key Set list, stacked beneath the KMS key
// table on the Encryption Keys page. A set is the published face of the key
// that backs it, which is why it lives here rather than on a page of its own.
export function SigningKeysSection({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): JSX.Element {
  const client = useGramContext();
  const [createOpen, setCreateOpen] = useState(false);
  const setsQuery = buildListJsonWebKeySetsQuery(client);
  const { data, isPending, isError, refetch } = useQuery({
    ...setsQuery,
    queryKey: [...setsQuery.queryKey, { organizationId }],
  });
  const sets = data?.sets ?? [];

  const createButton = (
    <RequireScope scope="org:admin" level="component">
      <Button size="sm" onClick={() => setCreateOpen(true)}>
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>New Signing Key Set</Button.Text>
      </Button>
    </RequireScope>
  );

  return (
    <>
      <ListSection
        eyebrow="Signing Keys (JWKS)"
        description={SECTION_DESCRIPTION}
        action={sets.length > 0 ? createButton : undefined}
      >
        <SetTable
          sets={sets}
          isPending={isPending}
          isError={isError}
          onRetry={() => void refetch()}
          createButton={createButton}
        />
      </ListSection>

      <CreateJsonWebKeySetSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        isCurrentOrganization={isCurrentOrganization}
      />
    </>
  );
}

function SetTable({
  sets,
  isPending,
  isError,
  onRetry,
  createButton,
}: {
  sets: JSONWebKeySet[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  createButton: React.ReactNode;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();

  if (isError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Text muted>Failed to load signing key sets.</Text>
        <Button size="sm" variant="secondary" onClick={onRetry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }

  if (isPending) {
    return (
      <Text muted className="py-8 text-center">
        Loading…
      </Text>
    );
  }

  if (sets.length === 0) {
    return (
      <InlineEmptyState
        icon="key-round"
        heading="No signing key sets yet"
        description="Create a set from one of your KMS keys to start publishing public keys for token verification. The set's first key is published and activated as part of creating it."
        action={createButton}
      />
    );
  }

  const headers = [
    { label: "Name" },
    { label: "Encryption key" },
    { label: "Created" },
    { label: "Updated" },
  ];

  return (
    <DotTable headers={headers}>
      {sets.map((set) => (
        <DotRow
          key={set.id}
          icon={
            <Icon name="key-round" className="text-muted-foreground h-5 w-5" />
          }
          href={orgRoutes.signingKeySets.setDetail.href(set.id)}
          ariaLabel={`View signing key set ${set.name}`}
        >
          <td className="px-3 py-3">
            <Text
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {set.name}
            </Text>
          </td>
          {/* The link sits above the row link, so it has to stop the click from
              reaching the row's own navigation. */}
          <td
            className="relative z-20 px-3 py-3"
            onClick={(e) => e.stopPropagation()}
          >
            <ExternalKeyLink externalKeyId={set.externalKeyId} />
          </td>
          <td className="px-3 py-3">
            <Text small muted as="div">
              <HumanizeDateTime date={set.createdAt} />
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted as="div">
              <HumanizeDateTime date={set.updatedAt} />
            </Text>
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}
