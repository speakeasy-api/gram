import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useGcpSetupInfo } from "@gram/client/react-query/gcpSetupInfo";
import { Stack } from "@/components/ui/Stack";

// GcpGrantInstructions shows the grant a customer has to make in their own GCP
// project before Gram can impersonate a service account there. It is rendered
// both while creating a credential and on an existing credential's Overview,
// because the grant is a prerequisite of creating one and the most likely reason
// verification fails afterwards.
export function GcpGrantInstructions(): JSX.Element {
  const { data, isLoading, isError } = useGcpSetupInfo();

  if (isLoading) {
    return (
      <Text small muted>
        Loading setup details…
      </Text>
    );
  }

  // A failed read and a successful one that carries no email need different
  // copy: the first is retryable, the second is a property of the environment.
  if (isError) {
    return (
      <Text small muted>
        Could not load Gram's service account. Reload to try again.
      </Text>
    );
  }

  return (
    <Stack gap={2}>
      <Text small muted>
        Grant Gram's service account the{" "}
        <span className="font-mono">
          {data?.requiredRole ?? "roles/iam.serviceAccountTokenCreator"}
        </span>{" "}
        role on the service account you want Gram to impersonate.
      </Text>
      {data?.serviceAccountEmail ? (
        <GrantValue
          label="Gram service account"
          value={data.serviceAccountEmail}
        />
      ) : (
        <Text small muted>
          This environment cannot report Gram's service account. Contact support
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
