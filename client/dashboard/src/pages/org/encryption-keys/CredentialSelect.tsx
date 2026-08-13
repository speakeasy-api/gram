import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useListExternalCredentials } from "@gram/client/react-query/listExternalCredentials";
import { useEffect } from "react";
import { Link } from "react-router";

// CredentialSelect picks the external credential Gram authenticates with to
// reach a key. Shared by the create sheet and the detail page's Settings tab so
// both offer the same set and explain an empty one the same way.
//
// Scoped to GCP: a gcp_kms key must be backed by a gcp_iam credential, and the
// server refuses any other pairing, so offering the rest would only produce a
// rejected save.
export function CredentialSelect({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { data, isLoading, isError, refetch } = useListExternalCredentials({
    provider: "gcp_iam",
  });
  const credentials = data?.credentials ?? [];

  // A key whose credential was deleted still carries that id. Left alone the
  // Select would show its empty placeholder while the dead id sat in state, and
  // saving would resubmit it — so clear it once the live set is known and let
  // the caller's own "a credential is required" rule block the save.
  //
  // A failed request is not a live set. Clearing on one would discard a
  // perfectly good selection because the list happened not to load, and leave
  // the form unsavable until it does.
  const missing =
    !isLoading &&
    !isError &&
    value !== "" &&
    !credentials.some((c) => c.id === value);
  useEffect(() => {
    if (missing) onChange("");
  }, [missing, onChange]);

  if (isError) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label>External credential</Label>
        <Stack direction="horizontal" gap={2} align="center">
          <Text small muted>
            Failed to load external credentials.
          </Text>
          <Button size="sm" variant="secondary" onClick={() => void refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </Stack>
      </div>
    );
  }

  if (!isLoading && credentials.length === 0) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label>External credential</Label>
        <Text small muted>
          No external credentials yet. A key needs one to reach your KMS —{" "}
          <Link
            to={orgRoutes.externalServices.href()}
            className="underline underline-offset-2"
          >
            create one first
          </Link>
          .
        </Text>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <Label>External credential</Label>
      <Select value={value} onValueChange={onChange} disabled={isLoading}>
        <SelectTrigger>
          <SelectValue
            placeholder={isLoading ? "Loading…" : "Select a credential"}
          />
        </SelectTrigger>
        <SelectContent>
          {credentials.map((credential) => (
            <SelectItem key={credential.id} value={credential.id}>
              {credential.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
