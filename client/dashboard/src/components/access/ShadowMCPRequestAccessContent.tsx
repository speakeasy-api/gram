import { GramLogo } from "@/components/gram-logo";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { useCreateShadowMCPApprovalRequestMutation } from "@gram/client/react-query";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";

const REQUEST_TOKEN_STORAGE_KEY = "shadowMcpApprovalRequestToken";
const inFlightSubmissions = new Map<string, Promise<void>>();

type RequestAccessState =
  | "missing-token"
  | "authenticating"
  | "submitting"
  | "complete"
  | "error";

type SubmissionResult = "idle" | "complete" | "error";

export function ShadowMCPRequestAccessContent() {
  const session = useSession();
  const requestToken = getRequestToken();
  const [submissionResult, setSubmissionResult] =
    useState<SubmissionResult>("idle");
  const [retryCount, setRetryCount] = useState(0);
  const { mutateAsync: createApprovalRequest } =
    useCreateShadowMCPApprovalRequestMutation();

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
      setSubmissionResult("idle");
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
    if (!storedRequestToken || !session.session) return;

    let submission = inFlightSubmissions.get(storedRequestToken);
    if (!submission) {
      submission = createApprovalRequest({
        request: {
          createShadowMCPApprovalRequestForm: {
            requestToken: storedRequestToken,
          },
        },
      })
        .then(() => undefined)
        .finally(() => {
          inFlightSubmissions.delete(storedRequestToken);
        });
      inFlightSubmissions.set(storedRequestToken, submission);
    }

    let active = true;
    submission
      .then(() => {
        if (!active) return;
        setSubmissionResult("complete");
        sessionStorage.removeItem(REQUEST_TOKEN_STORAGE_KEY);
      })
      .catch(() => {
        if (!active) return;
        setSubmissionResult("error");
      });

    return () => {
      active = false;
    };
  }, [createApprovalRequest, retryCount, session.session, storedRequestToken]);

  const state = getRequestAccessState({
    hasSession: !!session.session,
    hasToken: !!storedRequestToken,
    submissionResult,
  });

  return (
    <div className="bg-background flex min-h-screen w-full flex-col items-center justify-center p-8">
      <Stack gap={8} align="center" className="w-full max-w-sm">
        <GramLogo className="w-25" variant="vertical" />
        <RequestAccessMessage
          state={state}
          isPending={state === "submitting"}
          onRetry={() => {
            setSubmissionResult("idle");
            setRetryCount((count) => count + 1);
          }}
        />
      </Stack>
    </div>
  );
}

function getRequestAccessState({
  hasSession,
  hasToken,
  submissionResult,
}: {
  hasSession: boolean;
  hasToken: boolean;
  submissionResult: SubmissionResult;
}): RequestAccessState {
  if (submissionResult === "complete") return "complete";
  if (submissionResult === "error") return "error";
  if (!hasToken) return "missing-token";
  if (!hasSession) return "authenticating";
  return "submitting";
}

function getRequestToken(): string | null {
  const hashParams = new URLSearchParams(
    window.location.hash.replace(/^#/, ""),
  );
  return hashParams.get("request_token") ?? hashParams.get("token");
}

function RequestAccessMessage({
  state,
  isPending,
  onRetry,
}: {
  state: RequestAccessState;
  isPending: boolean;
  onRetry: () => void;
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
            You can close this page.
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

  if (state === "missing-token") {
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

  if (state === "error") {
    return (
      <Stack gap={3} align="center">
        <div className="bg-destructive/10 flex h-11 w-11 items-center justify-center rounded-full">
          <Icon name="circle-x" className="text-destructive h-5 w-5" />
        </div>
        <Stack gap={1} align="center">
          <Type variant="subheading" className="text-center">
            Request failed
          </Type>
          <Type muted small className="text-center">
            We could not send this request. Check your connection and try again.
          </Type>
        </Stack>
        <Button variant="secondary" onClick={onRetry}>
          <Button.LeftIcon>
            <Icon name="refresh-cw" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Try again</Button.Text>
        </Button>
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
