import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type ListParams, type ListResult, type Mode } from "@/lib/devidp";

export const queryKeys = {
  organizations: ["organizations"] as const,
  users: ["users"] as const,
  memberships: ["memberships"] as const,
  currentUser: (mode: Mode) => ["currentUser", mode] as const,
};

async function listAll<T>(
  listPage: (params: ListParams) => Promise<ListResult<T>>,
): Promise<ListResult<T>> {
  const items: T[] = [];
  let cursor: string | undefined;
  do {
    const page = await listPage({ cursor, limit: 100 });
    items.push(...page.items);
    cursor = page.next_cursor || undefined;
  } while (cursor);
  return { items, next_cursor: "" };
}

export function useOrganizations() {
  return useQuery({
    queryKey: queryKeys.organizations,
    queryFn: () => listAll(api.organizations.list),
  });
}

export function useUsers() {
  return useQuery({
    queryKey: queryKeys.users,
    queryFn: () => listAll(api.users.list),
  });
}

export function useMemberships() {
  return useQuery({
    queryKey: queryKeys.memberships,
    queryFn: () => listAll(api.memberships.list),
  });
}

export function useCreateOrganization() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.organizations.create,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: queryKeys.organizations }),
  });
}

export function useDeleteOrganization() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.organizations.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.organizations });
      qc.invalidateQueries({ queryKey: queryKeys.memberships });
    },
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.users.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.users }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.users.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.users });
      qc.invalidateQueries({ queryKey: queryKeys.memberships });
    },
  });
}

export function useUpdateOrganization() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.organizations.update,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: queryKeys.organizations }),
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.users.update,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.users }),
  });
}

export function useUpdateMembership() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.memberships.update,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.memberships }),
  });
}

export function useCreateMembership() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.memberships.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.memberships }),
  });
}

export function useDeleteMembership() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.memberships.delete,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.memberships }),
  });
}

export function useCurrentUser(mode: Mode) {
  return useQuery({
    queryKey: queryKeys.currentUser(mode),
    queryFn: async () => {
      try {
        return await api.devIdp.getCurrentUser({ mode });
      } catch (e) {
        // 404 when no row yet for that mode — treat as null
        return null;
      }
    },
  });
}

export function useSetCurrentUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.devIdp.setCurrentUser,
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: queryKeys.currentUser(data.mode) });
      qc.invalidateQueries({ queryKey: ["gram-mode"] });
    },
  });
}

export function useClearCurrentUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.devIdp.clearCurrentUser,
    onSuccess: (_, vars) => {
      qc.invalidateQueries({ queryKey: queryKeys.currentUser(vars.mode) });
      qc.invalidateQueries({ queryKey: ["gram-mode"] });
    },
  });
}
