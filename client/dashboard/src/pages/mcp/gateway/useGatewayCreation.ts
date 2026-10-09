import { nextSortOrder } from "./memberRows";
import { getHttpStatusCode } from "@/lib/route-errors";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

// The backend's refusal when another member of the gateway already fronts the
// same tunnel or remote server (metamcp addMember). Other conflicts, such as
// approval or disabled distribution, stay retryable and are not matched here.
const BACKEND_SHARING_REFUSAL = "already fronts the same backend";
const HTTP_CONFLICT = 409;

function isGatewayBackendSharingRefusal(error: unknown): boolean {
  return (
    getHttpStatusCode(error) === HTTP_CONFLICT &&
    error instanceof Error &&
    error.message.includes(BACKEND_SHARING_REFUSAL)
  );
}

const GATEWAY_BACKEND_SHARING_MESSAGE =
  "Your server was created, but this gateway already includes an MCP server on the same backend (the same tunnel or remote server), and a gateway can include only one. Open the new server, or add a server on a different backend to the gateway.";

/** Keep creation separate from attachment: retrying must never create a server. */
export interface GatewayCreationFlow {
  gatewayId: string | null;
  createdServerId: string | null;
  attachmentError: string | null;
  /** The gateway refused the server for good; retrying cannot succeed. */
  attachmentRefused: boolean;
  isAttaching: boolean;
  complete: (mcpServerId: string) => Promise<void>;
  retry: () => Promise<void>;
  cancel: () => boolean;
}

export function useGatewayCreation(): GatewayCreationFlow {
  const [searchParams] = useSearchParams();
  const gatewayId = searchParams.get("attachToGateway") || null;
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();
  const [createdServerId, setCreatedServerId] = useState<string | null>(null);
  const [attachmentError, setAttachmentError] = useState<string | null>(null);
  const [attachmentRefused, setAttachmentRefused] = useState(false);
  const [isAttaching, setIsAttaching] = useState(false);
  const cancelled = useRef(false);
  const pending = useRef<Promise<void> | null>(null);

  const attach = async (mcpServerId: string) => {
    if (!gatewayId || cancelled.current) return;
    setIsAttaching(true);
    setAttachmentError(null);
    const listMembers = async () => {
      const { members } = await client.metaMcp.listMembers({
        metaMcpServerId: gatewayId,
      });
      return members;
    };
    try {
      // Fresh reads protect retries, including a committed write whose response
      // was lost. Never assume an error means the member was not added.
      const members = await listMembers();
      if (
        !members.some((member) => member.mcpServerId === mcpServerId) &&
        !cancelled.current
      ) {
        try {
          await client.metaMcp.addMember({
            addMetaMcpMemberForm: {
              metaMcpServerId: gatewayId,
              mcpServerId,
              sortOrder: nextSortOrder(members),
            },
          });
        } catch (error) {
          if (
            !(await listMembers()).some(
              (member) => member.mcpServerId === mcpServerId,
            )
          )
            throw error;
        }
      }
      await invalidateAllMetaMcpMembers(queryClient);
      if (!cancelled.current)
        void navigate(routes.mcp.gateway.overview.href(gatewayId));
    } catch (error) {
      if (isGatewayBackendSharingRefusal(error)) {
        setAttachmentRefused(true);
        setAttachmentError(GATEWAY_BACKEND_SHARING_MESSAGE);
        throw error;
      }
      setAttachmentError(
        "Your server was created, but adding it to the gateway could not be confirmed. Retry to finish without creating another server.",
      );
      throw error;
    } finally {
      setIsAttaching(false);
    }
  };
  const complete = (mcpServerId: string): Promise<void> => {
    if (!gatewayId || cancelled.current || attachmentRefused) {
      return Promise.resolve();
    }
    if (pending.current) return pending.current;
    setCreatedServerId(mcpServerId);
    pending.current = attach(mcpServerId).finally(() => {
      pending.current = null;
    });
    return pending.current;
  };
  return {
    gatewayId,
    createdServerId,
    attachmentError,
    attachmentRefused,
    isAttaching,
    complete,
    retry: () =>
      createdServerId ? complete(createdServerId) : Promise.resolve(),
    cancel: () => {
      if (!gatewayId) return false;
      cancelled.current = true;
      void navigate(routes.mcp.gateway.overview.href(gatewayId));
      return true;
    },
  };
}
