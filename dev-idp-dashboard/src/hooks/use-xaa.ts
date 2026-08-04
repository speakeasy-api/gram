import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/devidp";

export const xaaQueryKeys = {
  apps: ["xaaApps"] as const,
  resources: ["xaaResources"] as const,
  assignments: ["xaaAppAssignments"] as const,
  trustRules: ["xaaTrustRules"] as const,
  issuedGrants: ["xaaIssuedGrants"] as const,
};

export function useXaaApps() {
  return useQuery({
    queryKey: xaaQueryKeys.apps,
    queryFn: () => api.xaaApps.list({ limit: 100 }),
  });
}

export function useXaaResources() {
  return useQuery({
    queryKey: xaaQueryKeys.resources,
    queryFn: () => api.xaaResources.list({ limit: 100 }),
  });
}

export function useXaaAssignments() {
  return useQuery({
    queryKey: xaaQueryKeys.assignments,
    queryFn: () => api.xaaAppAssignments.list({ limit: 100 }),
  });
}

export function useXaaTrustRules() {
  return useQuery({
    queryKey: xaaQueryKeys.trustRules,
    queryFn: () => api.xaaTrustRules.list({ limit: 100 }),
  });
}

export function useXaaIssuedGrants() {
  return useQuery({
    queryKey: xaaQueryKeys.issuedGrants,
    queryFn: () => api.xaaTrustRules.listIssuedGrants({ limit: 50 }),
  });
}

export function useCreateXaaApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaApps.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: xaaQueryKeys.apps }),
  });
}

export function useUpdateXaaApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaApps.update,
    onSuccess: () => qc.invalidateQueries({ queryKey: xaaQueryKeys.apps }),
  });
}

export function useDeleteXaaApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaApps.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: xaaQueryKeys.apps });
      // Deleting an app cascades to its assignments and issued grants.
      qc.invalidateQueries({ queryKey: xaaQueryKeys.assignments });
      qc.invalidateQueries({ queryKey: xaaQueryKeys.issuedGrants });
    },
  });
}

export function useCreateXaaResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaResources.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: xaaQueryKeys.resources }),
  });
}

export function useUpdateXaaResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaResources.update,
    onSuccess: () => qc.invalidateQueries({ queryKey: xaaQueryKeys.resources }),
  });
}

export function useDeleteXaaResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaResources.delete,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: xaaQueryKeys.resources });
      // Cascades to trust rules, assignments, and issued grants.
      qc.invalidateQueries({ queryKey: xaaQueryKeys.trustRules });
      qc.invalidateQueries({ queryKey: xaaQueryKeys.assignments });
      qc.invalidateQueries({ queryKey: xaaQueryKeys.issuedGrants });
    },
  });
}

export function useCreateXaaAssignment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaAppAssignments.create,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: xaaQueryKeys.assignments }),
  });
}

export function useUpdateXaaAssignment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaAppAssignments.update,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: xaaQueryKeys.assignments }),
  });
}

export function useDeleteXaaAssignment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaAppAssignments.delete,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: xaaQueryKeys.assignments }),
  });
}

export function useCreateXaaTrustRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaTrustRules.create,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: xaaQueryKeys.trustRules }),
  });
}

export function useUpdateXaaTrustRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaTrustRules.update,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: xaaQueryKeys.trustRules }),
  });
}

export function useDeleteXaaTrustRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.xaaTrustRules.delete,
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: xaaQueryKeys.trustRules }),
  });
}
