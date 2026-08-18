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
import { useListExternalCredentials } from "@gram/client/react-query/listExternalCredentials";
import { Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { CreateExternalCredentialSheet } from "../external-services/CreateExternalCredentialSheet";

// CredentialSelect picks the external credential Speakeasy authenticates with to
// reach a key. Shared by the create sheet and the detail page's Settings tab so
// both offer the same set and explain an empty one the same way.
//
// A credential can be created from here rather than only from External Services.
// A key is unusable without one, so an organization's first key would otherwise
// dead-end on a picker with nothing in it and a link to another page, losing
// whatever had been filled in so far.
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
  const [createOpen, setCreateOpen] = useState(false);
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

  // Select what was just created. The list query is invalidated by the sheet, so
  // the option is there by the time this renders again.
  const createSheet = (
    <CreateExternalCredentialSheet
      open={createOpen}
      onOpenChange={setCreateOpen}
      onCreated={(credential) => onChange(credential.id)}
    />
  );

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
          A key needs a credential to reach your KMS. Create one now and it will
          be selected here.
        </Text>
        <div>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setCreateOpen(true)}
          >
            <Button.LeftIcon>
              <Plus />
            </Button.LeftIcon>
            <Button.Text>New external credential</Button.Text>
          </Button>
        </div>
        {createSheet}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <Label>External credential</Label>
      <Stack direction="horizontal" gap={2} align="center">
        <div className="flex-1">
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
        <Button
          size="sm"
          variant="secondary"
          disabled={isLoading}
          onClick={() => setCreateOpen(true)}
        >
          <Button.LeftIcon>
            <Plus />
          </Button.LeftIcon>
          <Button.Text>New</Button.Text>
        </Button>
      </Stack>
      {createSheet}
    </div>
  );
}
