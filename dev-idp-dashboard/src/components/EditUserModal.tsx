import { useMemo, useState } from "react";
import { motion } from "motion/react";
import { useForm } from "@tanstack/react-form";
import { Pencil, Plus } from "lucide-react";
import { matchSorter } from "@/lib/fuzzy";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import {
  useCreateMembership,
  useDeleteUser,
  useMemberships,
  useOrganizations,
  useUpdateUser,
} from "@/hooks/use-devidp";
import type { Membership, User } from "@/lib/devidp";
import { EditModal } from "@/components/EditModal";
import { EditMembershipModal } from "@/components/EditMembershipModal";
import { ModalActions } from "@/components/ModalActions";

const membershipLayoutId = (id: string) => `membership-${id}`;

export function EditUserModal({
  user,
  layoutId,
  onClose,
}: {
  user: User;
  layoutId: string;
  onClose: () => void;
}) {
  const update = useUpdateUser();
  const remove = useDeleteUser();
  const orgsQ = useOrganizations();
  const membershipsQ = useMemberships();
  const addMembership = useCreateMembership();

  const allOrgs = orgsQ.data?.items ?? [];
  const allMemberships = membershipsQ.data?.items ?? [];
  const orgsById = useMemo(
    () => new Map(allOrgs.map((o) => [o.id, o])),
    [allOrgs],
  );
  const userMemberships = allMemberships.filter((m) => m.user_id === user.id);
  const memberOrgIds = new Set(userMemberships.map((m) => m.organization_id));

  const form = useForm({
    defaultValues: {
      email: user.email,
      display_name: user.display_name,
      admin: user.admin,
      whitelisted: user.whitelisted,
      github_handle: user.github_handle ?? "",
      photo_url: user.photo_url ?? "",
    },
    onSubmit: async ({ value }) => {
      await update.mutateAsync({ id: user.id, ...value });
      onClose();
    },
  });

  const [search, setSearch] = useState("");
  const candidates = useMemo(() => {
    const remaining = allOrgs.filter((o) => !memberOrgIds.has(o.id));
    if (!search.trim()) return remaining;
    return matchSorter(remaining, search, [(o) => o.name, (o) => o.slug]);
  }, [allOrgs, memberOrgIds, search]);

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
              User
            </div>
            <h2 className="text-lg font-semibold leading-tight">
              {user.display_name}
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
                onDelete={() =>
                  remove.mutate({ id: user.id }, { onSuccess: onClose })
                }
                canSave={canSubmit && isDirty}
                saving={isSubmitting}
                deleting={remove.isPending}
                deleteLabel="Delete user"
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
          <form.Field name="display_name">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>Display name</Label>
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
          <form.Field name="email">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>Email</Label>
                <Input
                  id={field.name}
                  type="email"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  required
                />
              </div>
            )}
          </form.Field>
          <form.Field name="github_handle">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>GitHub handle</Label>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                />
              </div>
            )}
          </form.Field>
          <form.Field name="photo_url">
            {(field) => (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor={field.name}>Photo URL</Label>
                <Input
                  id={field.name}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                />
              </div>
            )}
          </form.Field>
          <div className="flex gap-4">
            <form.Field name="admin">
              {(field) => (
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={field.state.value}
                    onChange={(e) => field.handleChange(e.target.checked)}
                  />
                  Admin
                </label>
              )}
            </form.Field>
            <form.Field name="whitelisted">
              {(field) => (
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <input
                    type="checkbox"
                    checked={field.state.value}
                    onChange={(e) => field.handleChange(e.target.checked)}
                  />
                  Whitelisted
                </label>
              )}
            </form.Field>
          </div>
          {update.error && (
            <div className="text-xs text-destructive">
              {(update.error as Error).message}
            </div>
          )}
        </form>

        <section className="mt-6 pt-6 border-t border-border space-y-3">
          <div className="flex items-baseline justify-between">
            <h3 className="text-sm font-semibold">Organizations</h3>
            <span className="text-xs text-muted-foreground">
              {userMemberships.length}
            </span>
          </div>
          <ul className="space-y-1.5">
            {userMemberships.map((m) => {
              const o = orgsById.get(m.organization_id);
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
                      {o?.name ?? m.organization_id}
                    </div>
                    {o?.slug && (
                      <div className="text-xs text-muted-foreground truncate">
                        {o.slug}
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
            {userMemberships.length === 0 && (
              <div className="text-xs text-muted-foreground italic">
                Not a member of any org.
              </div>
            )}
          </ul>
        </section>

        <section className="mt-6 pt-6 border-t border-border space-y-2">
          <Label htmlFor="add-org-search" className="text-sm font-semibold">
            Join organization
          </Label>
          <Input
            id="add-org-search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search organizations by name or slug…"
          />
          <ul className="space-y-1 max-h-48 overflow-y-auto">
            {candidates.slice(0, 12).map((o) => (
              <li
                key={o.id}
                className="flex items-center gap-2 rounded-md hover:bg-muted/40 px-2 py-1.5"
              >
                <div className="min-w-0 flex-1">
                  <div className="text-sm truncate">{o.name}</div>
                  <div className="text-xs text-muted-foreground truncate">
                    {o.slug}
                  </div>
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  onClick={() =>
                    addMembership.mutate(
                      { user_id: user.id, organization_id: o.id },
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
          subjectLabel={`${user.display_name} in ${
            orgsById.get(editingMembership.organization_id)?.name ??
            editingMembership.organization_id
          }`}
          onClose={() => setEditingMembership(null)}
        />
      )}
    </>
  );
}
