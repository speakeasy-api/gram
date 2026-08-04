import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/devidp";

export const emaQueryKeys = {
  apps: ["emaApps"] as const,
  resources: ["emaResources"] as const,
  assignments: ["emaAppAssignments"] as const,
  trustRules: ["emaTrustRules"] as const,
  issuedGrants: ["emaIssuedGrants"] as const,
};

export function useEmaApps() {
  return useQuery({
    queryKey: emaQueryKeys.apps,
    queryFn: () => api.emaApps.list({ limit: 100 }),
  });
}

export function useEmaResources() {
  return useQuery({
    queryKey: emaQueryKeys.resources,
    queryFn: () => api.emaResources.list({ limit: 100 }),
  });
}

export function useEmaAssignments() {
  return useQuery({
    queryKey: emaQueryKeys.assignments,
    queryFn: () => api.emaAppAssignments.list({ limit: 100 }),
  });
}

export function useEmaTrustRules() {
  return useQuery({
    queryKey: emaQueryKeys.trustRules,
    queryFn: () => api.emaTrustRules.list({ limit: 100 }),
  });
}

export function useEmaIssuedGrants() {
  return useQuery({
    queryKey: emaQueryKeys.issuedGrants,
    queryFn: () => api.emaTrustRules.listIssuedGrants({ limit: 50 }),
  });
}

export function useCreateEmaApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaApps.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: emaQueryKeys.apps }),
  });
}

export function useUpdateEmaApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaApps.update,
    onSuccess: () => qc.invalidateQueries({ queryKey: emaQueryKeys.apps }),
  });
}

export function useDeleteEmaApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaApps.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: emaQueryKeys.apps });
      // Deleting an app cascades to its assignments and issued grants.
      qc.invalidateQueries({ queryKey: emaQueryKeys.assignments });
      qc.invalidateQueries({ queryKey: emaQueryKeys.issuedGrants });
    },
  });
}

export function useCreateEmaResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaResources.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: emaQueryKeys.resources }),
  });
}

export function useUpdateEmaResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaResources.update,
    onSuccess: () => qc.invalidateQueries({ queryKey: emaQueryKeys.resources }),
  });
}

export function useDeleteEmaResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaResources.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: emaQueryKeys.resources });
      // Cascades to trust rules, assignments, and issued grants.
      qc.invalidateQueries({ queryKey: emaQueryKeys.trustRules });
      qc.invalidateQueries({ queryKey: emaQueryKeys.assignments });
      qc.invalidateQueries({ queryKey: emaQueryKeys.issuedGrants });
    },
  });
}

export function useCreateEmaAssignment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaAppAssignments.create,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: emaQueryKeys.assignments }),
  });
}

export function useUpdateEmaAssignment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaAppAssignments.update,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: emaQueryKeys.assignments }),
  });
}

export function useDeleteEmaAssignment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaAppAssignments.delete,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: emaQueryKeys.assignments }),
  });
}

export function useCreateEmaTrustRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaTrustRules.create,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: emaQueryKeys.trustRules }),
  });
}

export function useUpdateEmaTrustRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaTrustRules.update,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: emaQueryKeys.trustRules }),
  });
}

export function useDeleteEmaTrustRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.emaTrustRules.delete,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: emaQueryKeys.trustRules }),
  });
}
