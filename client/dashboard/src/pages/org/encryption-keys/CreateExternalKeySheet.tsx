import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useCreateGcpKmsKeyMutation } from "@gram/client/react-query/createGcpKmsKey";
import { invalidateAllListExternalKeys } from "@gram/client/react-query/listExternalKeys";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { CredentialSelect } from "./CredentialSelect";
import {
  DEFAULT_KEY_ALGORITHM,
  KEY_ALGORITHMS,
  type KeyAlgorithm,
  KMS_KEY_PROVIDERS,
  type KmsKeyProvider,
  providerSlug,
} from "./providers";

// CreateExternalKeySheet registers a key that already exists in the customer's
// KMS. Gram never creates the key material — this records where it is and how to
// reach it.
export function CreateExternalKeySheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();

  const [provider, setProvider] = useState<KmsKeyProvider>("gcp_kms");
  const [name, setName] = useState("");
  const [credentialId, setCredentialId] = useState("");
  const [algorithm, setAlgorithm] = useState<KeyAlgorithm>(
    DEFAULT_KEY_ALGORITHM,
  );
  const [resourceName, setResourceName] = useState("");

  const createMutation = useCreateGcpKmsKeyMutation({
    onSuccess: async (created) => {
      await invalidateAllListExternalKeys(queryClient);
      toast.success("Encryption key created");
      onOpenChange(false);
      orgRoutes.encryptionKeys.keyDetail.goTo(
        providerSlug(created.provider),
        created.id,
      );
    },
    onError: (error) => {
      console.error("Create external key failed", error);
    },
  });

  const submitting = createMutation.isPending;
  const submitError = createMutation.error
    ? createMutation.error instanceof Error && createMutation.error.message
      ? createMutation.error.message
      : "An unexpected error occurred. Please try again."
    : null;
  const { reset: resetCreateMutation } = createMutation;

  // Reset transient state whenever the sheet is reopened so a prior draft never
  // leaks into a new creation.
  useEffect(() => {
    if (!open) return;
    setProvider("gcp_kms");
    setName("");
    setCredentialId("");
    setAlgorithm(DEFAULT_KEY_ALGORITHM);
    setResourceName("");
    resetCreateMutation();
  }, [open, resetCreateMutation]);

  const submittable =
    name.trim().length > 0 &&
    credentialId.length > 0 &&
    resourceName.trim().length > 0;

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    createMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createGcpKmsKeyForm: {
          name: name.trim(),
          externalCredentialId: credentialId,
          algorithm,
          resourceName: resourceName.trim(),
        },
      },
    });
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">New KMS Key</SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Stack gap={4}>
            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">Provider</Label>
              <Select
                value={provider}
                onValueChange={(value) => setProvider(value as KmsKeyProvider)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KMS_KEY_PROVIDERS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Stack>

            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">Name</Label>
              <Input
                value={name}
                onChange={setName}
                placeholder="Production signing key"
              />
            </Stack>

            <CredentialSelect value={credentialId} onChange={setCredentialId} />

            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">Algorithm</Label>
              <Select
                value={algorithm}
                onValueChange={(value) => setAlgorithm(value as KeyAlgorithm)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KEY_ALGORITHMS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Text small muted>
                Must match what the key actually signs with. Verify reports a
                mismatch rather than letting Speakeasy publish something no
                verifier accepts.
              </Text>
            </Stack>

            {provider === "gcp_kms" && (
              <Stack gap={2}>
                <Label className="text-muted-foreground text-xs">
                  Resource name
                </Label>
                <Input
                  value={resourceName}
                  onChange={setResourceName}
                  placeholder="projects/p/locations/l/keyRings/r/cryptoKeys/k/cryptoKeyVersions/1"
                />
                <Text small muted>
                  A specific crypto key version. Asymmetric signing keys have no
                  primary version, so the version has to be named. The resource
                  name and algorithm are fixed once saved.
                </Text>
              </Stack>
            )}
          </Stack>

          {submitError && (
            <Alert variant="error" dismissible={false}>
              {submitError}
            </Alert>
          )}
        </div>

        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            variant="secondary"
            disabled={submitting}
            onClick={() => onOpenChange(false)}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!submittable || submitting}
            onClick={handleSubmit}
          >
            <Button.Text>{submitting ? "Creating…" : "Create"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
