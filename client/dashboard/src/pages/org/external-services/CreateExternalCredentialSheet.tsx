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
import { useOrgRoutes } from "@/routes";
import { useCreateGcpIamCredentialMutation } from "@gram/client/react-query/createGcpIamCredential";
import { invalidateAllListExternalCredentials } from "@gram/client/react-query/listExternalCredentials";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { GcpGrantInstructions } from "./GcpGrantInstructions";
import {
  EXTERNAL_SERVICE_PROVIDERS,
  type ExternalServiceProvider,
  providerSlug,
} from "./providers";

// CreateExternalCredentialSheet creates an organization external credential. The
// External Service selector chooses the provider and swaps which fields render.
export function CreateExternalCredentialSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();

  const [provider, setProvider] = useState<ExternalServiceProvider>("gcp_iam");
  const [name, setName] = useState("");
  const [impersonateServiceAccount, setImpersonateServiceAccount] =
    useState("");

  const createMutation = useCreateGcpIamCredentialMutation({
    onSuccess: async (created) => {
      await invalidateAllListExternalCredentials(queryClient);
      toast.success("External credential created");
      onOpenChange(false);
      orgRoutes.externalServices.credentialDetail.goTo(
        providerSlug(created.provider),
        created.id,
      );
    },
    onError: (error) => {
      console.error("Create external credential failed", error);
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
    setProvider("gcp_iam");
    setName("");
    setImpersonateServiceAccount("");
    resetCreateMutation();
  }, [open, resetCreateMutation]);

  // Impersonation is required: without a target there is nothing Gram could
  // assume and nothing verification could prove.
  const submittable =
    name.trim().length > 0 && impersonateServiceAccount.trim().length > 0;

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    createMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createGcpIamCredentialForm: {
          name: name.trim(),
          impersonateServiceAccount: impersonateServiceAccount.trim(),
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
          <SheetTitle className="text-lg font-semibold">
            New External Credential
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Stack gap={4}>
            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">
                External Service
              </Label>
              <Select
                value={provider}
                onValueChange={(value) =>
                  setProvider(value as ExternalServiceProvider)
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {EXTERNAL_SERVICE_PROVIDERS.map((option) => (
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
                placeholder="Production encryption key access"
              />
            </Stack>

            {provider === "gcp_iam" && (
              <GcpCredentialFields
                impersonateServiceAccount={impersonateServiceAccount}
                onImpersonateServiceAccountChange={setImpersonateServiceAccount}
              />
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

function GcpCredentialFields({
  impersonateServiceAccount,
  onImpersonateServiceAccountChange,
}: {
  impersonateServiceAccount: string;
  onImpersonateServiceAccountChange: (value: string) => void;
}): JSX.Element {
  return (
    <Stack gap={4}>
      <GcpGrantInstructions />
      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          Impersonate service account
        </Label>
        <Input
          value={impersonateServiceAccount}
          onChange={onImpersonateServiceAccountChange}
          placeholder="name@project.iam.gserviceaccount.com"
        />
      </Stack>
    </Stack>
  );
}
