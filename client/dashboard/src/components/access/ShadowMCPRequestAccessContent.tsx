import { GramLogo } from "@/components/gram-logo";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { useCreateShadowMCPApprovalRequestMutation } from "@gram/client/react-query";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { useEffect, useRef } from "react";
import { useSearchParams } from "react-router";

const REQUEST_TOKEN_STORAGE_KEY = "shadowMcpApprovalRequestToken";

type RequestAccessState =
  | "missing-token"
  | "authenticating"
  | "submitting"
  | "complete"
  | "error";

export function ShadowMCPRequestAccessContent() {
  const [searchParams] = useSearchParams();
  const session = useSession();
  const requestToken = getRequestToken(searchParams);
  const hasSubmitted = useRef(false);
  const {
    mutate: createApprovalRequest,
    isPending,
    isSuccess,
    isError,
  } = useCreateShadowMCPApprovalRequestMutation();

  useEffect(() => {
    const meta = document.createElement("meta");
    meta.name = "referrer";
    meta.content = "no-referrer";
    document.head.appendChild(meta);
    return () => {
      document.head.removeChild(meta);
    };
  }, []);

  useEffect(() => {
    if (requestToken) {
      sessionStorage.setItem(REQUEST_TOKEN_STORAGE_KEY, requestToken);
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, [requestToken]);

  const storedRequestToken =
    requestToken ?? sessionStorage.getItem(REQUEST_TOKEN_STORAGE_KEY);

  useEffect(() => {
    if (!storedRequestToken || session.session) return;

    window.location.href = buildLoginRedirectURL("/shadow-mcp/request");
  }, [session.session, storedRequestToken]);

  useEffect(() => {
    if (!storedRequestToken || !session.session || hasSubmitted.current) return;

    hasSubmitted.current = true;
    createApprovalRequest(
      {
        request: {
          createShadowMCPApprovalRequestForm: {
            requestToken: storedRequestToken,
          },
        },
      },
      {
        onSuccess: () => {
          sessionStorage.removeItem(REQUEST_TOKEN_STORAGE_KEY);
        },
      },
    );
  }, [createApprovalRequest, session.session, storedRequestToken]);

  const state: RequestAccessState = !storedRequestToken
    ? "missing-token"
    : !session.session
      ? "authenticating"
      : isSuccess
        ? "complete"
        : isError
          ? "error"
          : "submitting";

  return (
    <div className="bg-background flex min-h-screen w-full flex-col items-center justify-center p-8">
      <Stack gap={8} align="center" className="w-full max-w-sm">
        <GramLogo className="w-25" variant="vertical" />
        <RequestAccessMessage state={state} isPending={isPending} />
      </Stack>
    </div>
  );
}

function getRequestToken(searchParams: URLSearchParams): string | null {
  const queryToken =
    searchParams.get("request_token") ?? searchParams.get("token");
  if (queryToken) return queryToken;

  const hashParams = new URLSearchParams(
    window.location.hash.replace(/^#/, ""),
  );
  return hashParams.get("request_token") ?? hashParams.get("token");
}

function RequestAccessMessage({
  state,
  isPending,
}: {
  state: RequestAccessState;
  isPending: boolean;
}) {
  if (state === "complete") {
    return (
      <Stack gap={3} align="center">
        <div className="bg-primary/10 flex h-11 w-11 items-center justify-center rounded-full">
          <Icon name="check" className="text-primary h-5 w-5" />
        </div>
        <Stack gap={1} align="center">
          <Type variant="subheading" className="text-center">
            Request sent
          </Type>
          <Type muted small className="text-center">
            Your admin has been notified. You can close this page.
          </Type>
        </Stack>
      </Stack>
    );
  }

  if (state === "authenticating") {
    return (
      <Stack gap={3} align="center">
        <Icon
          name="loader-circle"
          className="text-muted-foreground h-6 w-6 animate-spin"
        />
        <Type muted small className="text-center">
          Redirecting to sign in...
        </Type>
      </Stack>
    );
  }

  if (state === "missing-token" || state === "error") {
    return (
      <Stack gap={3} align="center">
        <div className="bg-destructive/10 flex h-11 w-11 items-center justify-center rounded-full">
          <Icon name="circle-x" className="text-destructive h-5 w-5" />
        </div>
        <Stack gap={1} align="center">
          <Type variant="subheading" className="text-center">
            Link expired
          </Type>
          <Type muted small className="text-center">
            This request link is no longer valid. Try the blocked MCP action
            again to generate a new request.
          </Type>
        </Stack>
      </Stack>
    );
  }

  return (
    <Stack gap={3} align="center">
      <Icon
        name="loader-circle"
        className="text-muted-foreground h-6 w-6 animate-spin"
      />
      <Type muted small className="text-center">
        {isPending ? "Submitting request..." : "Preparing request..."}
      </Type>
    </Stack>
  );
}
