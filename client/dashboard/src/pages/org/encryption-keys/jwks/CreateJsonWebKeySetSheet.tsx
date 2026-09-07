import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
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
import { useCreateJsonWebKeySetMutation } from "@gram/client/react-query/createJsonWebKeySet";
import { invalidateAllListJsonWebKeySets } from "@gram/client/react-query/listJsonWebKeySets";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { ExternalKeySelect } from "./ExternalKeySelect";

// CreateJsonWebKeySetSheet creates a signing key set from a KMS key. One request
// does the whole setup: the server reads the key's public half from the
// customer's KMS, creates the set, and publishes that key as the set's active
// signing key, so the set is ready for verifiers as soon as the sheet closes.
export function CreateJsonWebKeySetSheet({
  open,
  onOpenChange,
  isCurrentOrganization,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  isCurrentOrganization: () => boolean;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();

  const [name, setName] = useState("");
  const [externalKeyId, setExternalKeyId] = useState("");

  const createMutation = useCreateJsonWebKeySetMutation({
    onSuccess: async (created) => {
      if (!isCurrentOrganization()) return;
      await invalidateAllListJsonWebKeySets(queryClient);
      if (!isCurrentOrganization()) return;
      toast.success("Signing key set created");
      onOpenChange(false);
      orgRoutes.signingKeySets.setDetail.keys.goTo(created.id);
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      console.error("Create signing key set failed", error);
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
    setName("");
    setExternalKeyId("");
    resetCreateMutation();
  }, [open, resetCreateMutation]);

  const submittable = name.trim().length > 0 && externalKeyId.length > 0;

  const handleSubmit = () => {
    if (!submittable || submitting) return;
    createMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createJSONWebKeySetForm: {
          name: name.trim(),
          externalKeyId,
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
            New Signing Key Set
          </SheetTitle>
        </SheetHeader>

        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Text small muted>
            A JSON Web Key Set (JWKS) is the published collection of public keys
            that identity providers and MCP servers use to verify tokens
            Speakeasy signs. The private half never leaves your KMS.
          </Text>

          <Stack gap={4}>
            <Stack gap={2}>
              <Label className="text-muted-foreground text-xs">Name</Label>
              <Input
                value={name}
                onChange={setName}
                placeholder="Production signing keys"
              />
            </Stack>

            <ExternalKeySelect
              value={externalKeyId}
              onChange={setExternalKeyId}
              disabled={submitting}
            />
            <Text small muted>
              Creating the set publishes this key straight away as its active
              signing key. Speakeasy reads the key&apos;s public half from your
              KMS to do so, so the key&apos;s credential has to work now, not
              later.
            </Text>
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
