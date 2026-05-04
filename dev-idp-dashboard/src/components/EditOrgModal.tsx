import { useMemo, useState } from "react";
import { motion } from "motion/react";
import { useForm } from "@tanstack/react-form";
import { Pencil, Plus } from "lucide-react";
import { matchSorter } from "@/lib/fuzzy";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  useCreateMembership,
  useDeleteOrganization,
  useMemberships,
  useUpdateOrganization,
  useUsers,
} from "@/hooks/use-devidp";
import type { Membership, Organization } from "@/lib/devidp";
import { EditModal } from "@/components/EditModal";
import { EditMembershipModal } from "@/components/EditMembershipModal";
import { ModalActions } from "@/components/ModalActions";
import { ACCOUNT_TYPES } from "@/lib/edit-options";

const membershipLayoutId = (id: string) => `membership-${id}`;

export function EditOrgModal({
  org,
  layoutId,
  onClose,
}: {
  org: Organization;
  layoutId: string;
  onClose: () => void;
}) {
  const update = useUpdateOrganization();
  const remove = useDeleteOrganization();
  const usersQ = useUsers();
  const membershipsQ = useMemberships();
  const addMembership = useCreateMembership();

  const allUsers = usersQ.data?.items ?? [];
  const allMemberships = membershipsQ.data?.items ?? [];
  const usersById = useMemo(
    () => new Map(allUsers.map((u) => [u.id, u])),
    [allUsers],
  );
  const orgMemberships = allMemberships.filter(
    (m) => m.organization_id === org.id,
  );
  const memberUserIds = new Set(orgMemberships.map((m) => m.user_id));

  const form = useForm({
    defaultValues: {
      name: org.name,
      slug: org.slug,
      account_type: org.account_type,
    },
    onSubmit: async ({ value }) => {
      await update.mutateAsync({ id: org.id, ...value });
      onClose();
    },
  });

  const [search, setSearch] = useState("");
  const candidates = useMemo(() => {
    const remaining = allUsers.filter((u) => !memberUserIds.has(u.id));
    if (!search.trim()) return remaining;
    return matchSorter(remaining, search, [
      (u) => u.display_name,
      (u) => u.email,
    ]);
  }, [allUsers, memberUserIds, search]);

  const [editingMembership, setEditingMembership] = useState<Membership | null>(
    null,
  );

  return (
    <>
      <EditModal
        layoutId={layoutId}
        open
        onClose={onClose}
        title={
          <div>
            <div className="text-xs uppercase tracking-wider text-muted-foreground">
              Organization
            </div>
            <h2 className="text-lg font-semibold leading-tight">{org.name}</h2>
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
                onDelete={() =>
                  remove.mutate({ id: org.id }, { onSuccess: onClose })
                }
                canSave={canSubmit && isDirty}
                saving={isSubmitting}
                deleting={remove.isPending}
                deleteLabel="Delete organization"
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
          <form.Field name="name">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>Name</Label>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  required
                />
              </div>
            )}
          </form.Field>
          <form.Field name="slug">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>Slug</Label>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  required
                />
              </div>
            )}
          </form.Field>
          <form.Field name="account_type">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>Gram account type</Label>
                <Select
                  value={field.state.value}
                  onValueChange={(v) => field.handleChange(v ?? "")}
                >
                  <SelectTrigger id={field.name} className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {[
                      ...new Set([
                        field.state.value,
                        ...ACCOUNT_TYPES,
                      ] as string[]),
                    ].map((t) => (
                      <SelectItem key={t} value={t}>
                        {t}
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

        <section className="mt-6 pt-6 border-t border-border space-y-3">
          <div className="flex items-baseline justify-between">
            <h3 className="text-sm font-semibold">Members</h3>
            <span className="text-xs text-muted-foreground">
              {orgMemberships.length}
            </span>
          </div>
          <ul className="space-y-1.5">
            {orgMemberships.map((m) => {
              const u = usersById.get(m.user_id);
              return (
                <motion.li
                  key={m.id}
                  layoutId={membershipLayoutId(m.id)}
                  layout
                  initial={{ opacity: 0, x: -8 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: -8 }}
                  className="flex items-center gap-2 rounded-md bg-muted/40 px-2 py-1.5"
                >
                  <div className="min-w-0 flex-1">
                    <div className="text-sm truncate">
                      {u?.display_name ?? m.user_id}
                    </div>
                    {u?.email && (
                      <div className="text-xs text-muted-foreground truncate">
                        {u.email}
                      </div>
                    )}
                  </div>
                  <span className="text-xs text-muted-foreground font-mono uppercase tracking-wide">
                    {m.role}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => setEditingMembership(m)}
                    aria-label="Edit membership"
                  >
                    <Pencil />
                  </Button>
                </motion.li>
              );
            })}
            {orgMemberships.length === 0 && (
              <div className="text-xs text-muted-foreground italic">
                No members yet.
              </div>
            )}
          </ul>
        </section>

        <section className="mt-6 pt-6 border-t border-border space-y-2">
          <Label htmlFor="add-member-search" className="text-sm font-semibold">
            Add member
          </Label>
          <Input
            id="add-member-search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search users by name or email…"
          />
          <ul className="space-y-1 max-h-48 overflow-y-auto">
            {candidates.slice(0, 12).map((u) => (
              <li
                key={u.id}
                className="flex items-center gap-2 rounded-md hover:bg-muted/40 px-2 py-1.5"
              >
                <div className="min-w-0 flex-1">
                  <div className="text-sm truncate">{u.display_name}</div>
                  <div className="text-xs text-muted-foreground truncate">
                    {u.email}
                  </div>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  onClick={() =>
                    addMembership.mutate(
                      { user_id: u.id, organization_id: org.id },
                      { onSuccess: () => setSearch("") },
                    )
                  }
                >
                  <Plus /> Add
                </Button>
              </li>
            ))}
            {candidates.length === 0 && (
              <li className="text-xs text-muted-foreground italic px-2 py-1.5">
                No matches.
              </li>
            )}
          </ul>
        </section>
      </EditModal>
      {editingMembership && (
        <EditMembershipModal
          membership={editingMembership}
          layoutId={membershipLayoutId(editingMembership.id)}
          subjectLabel={`${
            usersById.get(editingMembership.user_id)?.display_name ??
            editingMembership.user_id
          } in ${org.name}`}
          onClose={() => setEditingMembership(null)}
        />
      )}
    </>
  );
}
