import { useForm } from "@tanstack/react-form";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useDeleteMembership, useUpdateMembership } from "@/hooks/use-devidp";
import type { Membership } from "@/lib/devidp";
import { EditModal } from "@/components/EditModal";
import { ModalActions } from "@/components/ModalActions";
import { ROLE_OPTIONS } from "@/lib/edit-options";

export function EditMembershipModal({
  membership,
  layoutId,
  subjectLabel,
  onClose,
}: {
  membership: Membership;
  layoutId: string;
  /** Plain-text context shown in the header, e.g. "Jim Halpert in Pied Piper". */
  subjectLabel: string;
  onClose: () => void;
}) {
  const update = useUpdateMembership();
  const remove = useDeleteMembership();

  const form = useForm({
    defaultValues: { role: membership.role },
    onSubmit: async ({ value }) => {
      await update.mutateAsync({ id: membership.id, role: value.role });
      onClose();
    },
  });

  return (
    <EditModal
      layoutId={layoutId}
      open
      onClose={onClose}
      level={1}
      title={
        <div>
          <div className="text-xs uppercase tracking-wider text-muted-foreground">
            Membership
          </div>
          <h2 className="text-lg font-semibold leading-tight">
            {subjectLabel}
          </h2>
        </div>
      }
      footer={
        <form.Subscribe
          selector={(s) => [s.canSubmit, s.isSubmitting, s.isDirty] as const}
        >
          {([canSubmit, isSubmitting, isDirty]) => (
            <ModalActions
              onCancel={onClose}
              onSave={() => form.handleSubmit()}
              onDelete={() => {
                remove.mutate({ id: membership.id }, { onSuccess: onClose });
              }}
              canSave={canSubmit && isDirty}
              saving={isSubmitting}
              deleting={remove.isPending}
              deleteLabel="Delete membership"
            />
          )}
        </form.Subscribe>
      }
    >
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault();
          form.handleSubmit();
        }}
      >
        <form.Field name="role">
          {(field) => (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={field.name}>Role</Label>
              <Select
                value={field.state.value}
                onValueChange={(v) => v && field.handleChange(v)}
              >
                <SelectTrigger id={field.name} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {[
                    ...new Set([
                      field.state.value,
                      ...ROLE_OPTIONS,
                    ] as string[]),
                  ].map((r) => (
                    <SelectItem key={r} value={r}>
                      {r}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
        </form.Field>
        {update.error && (
          <div className="text-xs text-destructive">
            {(update.error as Error).message}
          </div>
        )}
      </form>
    </EditModal>
  );
}
