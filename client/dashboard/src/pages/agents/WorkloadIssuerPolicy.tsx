import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { WorkloadIssuer } from "@gram/client/models/components/workloadissuer.js";
import { useUpdateWorkloadIssuerMutation } from "@gram/client/react-query/updateWorkloadIssuer.js";
import { invalidateAllWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { toast } from "sonner";

export function AssertionReuseField({
  checked,
  onChange,
  disabled = false,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}): JSX.Element {
  const descriptionId = useId();
  return (
    <div className="space-y-2 text-sm">
      <label className="flex items-center gap-2">
        <input
          type="checkbox"
          checked={checked}
          disabled={disabled}
          onChange={(event) => onChange(event.target.checked)}
          aria-describedby={descriptionId}
        />
        Allow assertion reuse
      </label>
      <p id={descriptionId} className="text-muted-foreground">
        Off by default: assertions with a jti or uti are single-use. Enable only
        when this issuer needs to reuse still-valid assertions, such as
        Kubernetes projected service account tokens. Signature, expiry,
        audience, and admission checks still apply. Tokens without jti or uti
        are unchanged. This setting affects every workload using this issuer,
        not just this agent.
      </p>
    </div>
  );
}

export function WorkloadIssuerPolicy({
  issuer,
}: {
  issuer: WorkloadIssuer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [allowAssertionReuse, setAllowAssertionReuse] = useState(false);
  const update = useUpdateWorkloadIssuerMutation({
    onSuccess: async () => {
      await invalidateAllWorkloadIdentities(queryClient, {
        refetchType: "all",
      });
      setOpen(false);
      toast.success("Issuer policy updated");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Could not update issuer policy",
      );
    },
  });

  return (
    <>
      <span className="text-muted-foreground text-xs">
        {issuer.allowAssertionReuse
          ? "Assertion reuse allowed"
          : "Assertion reuse off (jti/uti)"}
      </span>
      <RequireScope scope="workload:write" level="component">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            setAllowAssertionReuse(issuer.allowAssertionReuse ?? false);
            setOpen(true);
          }}
          aria-label={`Edit ${issuer.name || issuer.issuer} issuer policy`}
        >
          <Button.Text>Issuer policy</Button.Text>
        </Button>
      </RequireScope>
      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!update.isPending) setOpen(next);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              Issuer policy: {issuer.name || issuer.issuer}
            </Dialog.Title>
            <Dialog.Description>
              Change assertion handling for this trusted issuer across all its
              workloads.
            </Dialog.Description>
          </Dialog.Header>
          <AssertionReuseField
            checked={allowAssertionReuse}
            onChange={setAllowAssertionReuse}
            disabled={update.isPending}
          />
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={update.isPending}
              onClick={() => setOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <RequireScope scope="workload:write" level="component">
              <Button
                disabled={update.isPending}
                onClick={() =>
                  update.mutate({
                    request: {
                      updateWorkloadIssuerForm: {
                        id: issuer.id,
                        allowAssertionReuse,
                      },
                    },
                  })
                }
              >
                <Button.Text>
                  {update.isPending ? "Saving…" : "Save issuer policy"}
                </Button.Text>
              </Button>
            </RequireScope>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
