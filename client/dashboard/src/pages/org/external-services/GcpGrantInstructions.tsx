import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useGcpSetupInfo } from "@gram/client/react-query/gcpSetupInfo";
import { Stack } from "@/components/ui/Stack";
import {
  memberEmail,
  speakeasyPrincipalMember,
} from "@/pages/org/encryption-keys/jwks/gcpSetupSnippets";

// GcpGrantInstructions shows the grant a customer has to make in their own GCP
// project before Speakeasy can impersonate a service account there. It is rendered
// both while creating a credential and on an existing credential's Overview,
// because the grant is a prerequisite of creating one and the most likely reason
// verification fails afterwards.
export function GcpGrantInstructions(): JSX.Element {
  const { data, isLoading, isError } = useGcpSetupInfo();

  if (isLoading) {
    return (
      <Skeleton label="Loading setup details">
        <div className="h-4 w-full max-w-lg" />
        <div className="h-9 w-full max-w-md" />
      </Skeleton>
    );
  }

  // A failed read and a successful one that carries no email need different
  // copy: the first is retryable, the second is a property of the environment.
  if (isError) {
    return (
      <Text small muted>
        Could not load Speakeasy's principal. Reload to try again.
      </Text>
    );
  }

  // A group fronts Speakeasy's service account, so the grant names whichever
  // member the server reports.
  const principal = memberEmail(speakeasyPrincipalMember(data ?? {}));

  return (
    <Stack gap={2}>
      <Text small muted>
        Grant Speakeasy's principal the{" "}
        <span className="font-mono">
          {data?.requiredRole ?? "roles/iam.serviceAccountTokenCreator"}
        </span>{" "}
        role on the service account you want Speakeasy to impersonate.
      </Text>
      {principal ? (
        <GrantValue label="Speakeasy principal" value={principal} />
      ) : (
        <Text small muted>
          This environment cannot report Speakeasy's principal. Contact support
          for the principal to grant.
        </Text>
      )}
    </Stack>
  );
}

function GrantValue({
  label,
  value,
}: {
  label: string;
  value: string;
}): JSX.Element {
  return (
    <Stack gap={1}>
      <Text small muted>
        {label}
      </Text>
      <div className="bg-muted/50 flex items-center justify-between gap-2 p-3 font-mono text-sm">
        <code className="break-all">{value}</code>
        <CopyButton size="xs" text={value} tooltip={`Copy ${label}`} />
      </div>
    </Stack>
  );
}
