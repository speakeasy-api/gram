import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import type { UserSessionIssuerCimdClient } from "@gram/client/models/components/usersessionissuercimdclient.js";
import { useCreateOrganizationUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/createOrganizationUserSessionIssuerCimdClient.js";
import { useCreateUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/createUserSessionIssuerCimdClient.js";
import { useDeleteOrganizationUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/deleteOrganizationUserSessionIssuerCimdClient.js";
import { useDeleteUserSessionIssuerCimdClientMutation } from "@gram/client/react-query/deleteUserSessionIssuerCimdClient.js";
import { useOrganizationUserSessionIssuerCimdClientsInfinite } from "@gram/client/react-query/organizationUserSessionIssuerCimdClients.js";
import { useUserSessionIssuerCimdClientsInfinite } from "@gram/client/react-query/userSessionIssuerCimdClients.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
import { toast } from "sonner";
import { invalidateIssuers } from "./invalidateIssuers";
import type { EditPath } from "./providerSettings";

export type OrganizationClients = {
  /** Null when this page cannot read the list (another project's provider). */
  items: UserSessionIssuerCimdClient[] | null;
  isLoading: boolean;
  isError: boolean;
  retry: () => void;
  add: (url: string, onDone: () => void) => void;
  adding: boolean;
  addError: Error | null;
  /** Forgets a failed add, so its message does not outlive the attempt. */
  clearAddError: () => void;
  remove: (id: string, onDone: () => void) => void;
  removing: boolean;
};

/**
 * The client documents an organization allows on one provider, beyond
 * Speakeasy's catalog. An organization-wide provider keeps them through the
 * organization and a project's own through that project, so both lists are
 * set up and only the one this provider uses is read.
 */
export function useOrganizationClients(
  issuer: UserSessionIssuer,
  path: EditPath,
): OrganizationClients {
  const queryClient = useQueryClient();
  const params = { userSessionIssuerId: issuer.id };
  const organizationList = useOrganizationUserSessionIssuerCimdClientsInfinite(
    params,
    undefined,
    { enabled: path.kind === "organization", throwOnError: false },
  );
  const projectList = useUserSessionIssuerCimdClientsInfinite(
    params,
    undefined,
    { enabled: path.kind === "project", throwOnError: false },
  );
  const list = path.kind === "organization" ? organizationList : projectList;

  // Read every page: the list is shown whole. A failed page stops the walk,
  // since the last good page still reports a next one.
  const {
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  } = list;
  const readable = path.kind !== "elsewhere";
  useEffect(() => {
    if (
      readable &&
      hasNextPage &&
      !isFetchingNextPage &&
      !isFetchNextPageError
    ) {
      void fetchNextPage();
    }
  }, [
    readable,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  const items = useMemo(
    () => list.data?.pages.flatMap((page) => page.result.items) ?? [],
    [list.data],
  );

  const settle = async (message: string): Promise<void> => {
    await invalidateIssuers(queryClient);
    toast.success(message);
  };
  const createOrganization =
    useCreateOrganizationUserSessionIssuerCimdClientMutation();
  const createProject = useCreateUserSessionIssuerCimdClientMutation();
  const deleteOrganization =
    useDeleteOrganizationUserSessionIssuerCimdClientMutation();
  const deleteProject = useDeleteUserSessionIssuerCimdClientMutation();
  const create =
    path.kind === "organization" ? createOrganization : createProject;
  const remove =
    path.kind === "organization" ? deleteOrganization : deleteProject;

  return {
    items: readable ? items : null,
    isLoading:
      readable && (list.isLoading || (!!hasNextPage && !isFetchNextPageError)),
    isError: readable && (list.isError || isFetchNextPageError),
    retry: () => void list.refetch(),
    add: (url, onDone) => {
      const form = {
        createUserSessionIssuerCimdClientForm: {
          userSessionIssuerId: issuer.id,
          clientIdMetadataUri: url,
        },
      };
      const options = {
        onSuccess: async () => {
          await settle("Client added");
          onDone();
        },
      };
      if (path.kind === "organization") {
        createOrganization.mutate({ request: form }, options);
      } else if (path.kind === "project") {
        createProject.mutate({ request: form }, options);
      }
    },
    adding: create.isPending,
    addError: create.error,
    clearAddError: () => {
      // Resetting a pending add would drop its success handling while the
      // request still lands, so only a settled attempt is forgotten.
      if (!createOrganization.isPending) createOrganization.reset();
      if (!createProject.isPending) createProject.reset();
    },
    remove: (id, onDone) => {
      const options = {
        onSuccess: async () => {
          await settle("Client removed");
          onDone();
        },
        onError: (error: Error) => {
          toast.error(error.message || "Couldn't remove the client.");
        },
      };
      if (path.kind === "organization") {
        deleteOrganization.mutate({ request: { id } }, options);
      } else if (path.kind === "project") {
        deleteProject.mutate({ request: { id } }, options);
      }
    },
    removing: remove.isPending,
  };
}
