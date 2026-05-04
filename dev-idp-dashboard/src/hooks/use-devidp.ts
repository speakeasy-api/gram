import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type Mode } from "@/lib/devidp";

export const queryKeys = {
  organizations: ["organizations"] as const,
  users: ["users"] as const,
  memberships: ["memberships"] as const,
  currentUser: (mode: Mode) => ["currentUser", mode] as const,
};

export function useOrganizations() {
  return useQuery({
    queryKey: queryKeys.organizations,
    queryFn: () => api.organizations.list({ limit: 100 }),
  });
}

export function useUsers() {
  return useQuery({
    queryKey: queryKeys.users,
    queryFn: () => api.users.list({ limit: 100 }),
  });
}

export function useMemberships() {
  return useQuery({
    queryKey: queryKeys.memberships,
    queryFn: () => api.memberships.list({ limit: 100 }),
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
    },
  });
}
