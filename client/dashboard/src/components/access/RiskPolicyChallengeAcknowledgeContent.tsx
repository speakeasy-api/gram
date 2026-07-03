import { GramLogo } from "@/components/gram-logo";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useEffect, useState } from "react";
import { useRiskAcknowledgePolicyChallengeMutation } from "./useRiskAcknowledgePolicyChallengeMutation";

const ACK_TOKEN_STORAGE_KEY = "riskPolicyChallengeAckToken";
const inFlightSubmissions = new Map<string, Promise<void>>();

type AcknowledgeState =
  | "missing-token"
  | "authenticating"
  | "submitting"
  | "complete"
  | "declined"
  | "error";

type SubmissionResult = "idle" | "complete" | "error";

export function RiskPolicyChallengeAcknowledgeContent(): JSX.Element {
  const session = useSession();
  const ackToken = getAckToken();
  const [submissionResult, setSubmissionResult] =
    useState<SubmissionResult>("idle");
  const [declined, setDeclined] = useState(false);
  const [retryCount, setRetryCount] = useState(0);
  const { mutateAsync: acknowledgeChallenge, isPending } =
    useRiskAcknowledgePolicyChallengeMutation();

  // The ack token is a bearer credential delivered in the URL fragment (see
  // GeneratePolicyChallengeAckURL on the server) so it never hits server logs.
  // Force no-referrer so it also can't leak via the Referer header to anything
  // this page loads.
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
    if (ackToken) {
      setSubmissionResult("idle");
      // Move the token out of the URL into sessionStorage and immediately strip
      // the fragment from the address bar / history, so it isn't left sitting in
      // a shared screen, browser history, or a copy-pasted URL.
      sessionStorage.setItem(ACK_TOKEN_STORAGE_KEY, ackToken);
      // Drop only the token-bearing fragment; preserve any query string so other
      // routing/context params survive.
      window.history.replaceState(
        null,
        "",
        window.location.pathname + window.location.search,
      );
    }
  }, [ackToken]);

  const storedAckToken =
    ackToken ?? sessionStorage.getItem(ACK_TOKEN_STORAGE_KEY);

  useEffect(() => {
    if (!storedAckToken || session.session) return;

    window.location.href = buildLoginRedirectURL(window.location.pathname);
  }, [session.session, storedAckToken]);

  useEffect(() => {
    if (!storedAckToken || !session.session || declined) return;

    let submission = inFlightSubmissions.get(storedAckToken);
    if (!submission) {
      submission = acknowledgeChallenge({
        request: {
          acknowledgeRiskPolicyChallengeRequestBody: {
            ackToken: storedAckToken,
          },
        },
      })
        .then(() => undefined)
        .finally(() => {
          inFlightSubmissions.delete(storedAckToken);
        });
      inFlightSubmissions.set(storedAckToken, submission);
    }

    let active = true;
    submission
      .then(() => {
        if (!active) return;
        setSubmissionResult("complete");
        sessionStorage.removeItem(ACK_TOKEN_STORAGE_KEY);
      })
      .catch(() => {
        if (!active) return;
        setSubmissionResult("error");
      });

    return () => {
      active = false;
    };
  }, [
    acknowledgeChallenge,
    declined,
    retryCount,
    session.session,
    storedAckToken,
  ]);

  const state = getAcknowledgeState({
    hasSession: !!session.session,
    hasToken: !!storedAckToken,
    declined,
    submissionResult,
  });

  return (
    <div className="bg-background flex min-h-screen w-full flex-col items-center justify-center p-8">
      <Stack gap={8} align="center" className="w-full max-w-sm">
        <GramLogo className="w-25" variant="vertical" />
        <AcknowledgeMessage
          state={state}
          isPending={isPending}
          onRetry={() => {
            setSubmissionResult("idle");
            setRetryCount((count) => count + 1);
          }}
          onDecline={() => {
            // Decline is client-side only: the unclicked challenge row expires
            // server-side. We just drop the token and navigate to a terminal
            // "declined" state so the user knows nothing was acknowledged.
            sessionStorage.removeItem(ACK_TOKEN_STORAGE_KEY);
            setDeclined(true);
          }}
        />
      </Stack>
    </div>
  );
}

function getAcknowledgeState({
  hasSession,
  hasToken,
  declined,
  submissionResult,
}: {
  hasSession: boolean;
  hasToken: boolean;
  declined: boolean;
  submissionResult: SubmissionResult;
}): AcknowledgeState {
  if (declined) return "declined";
  if (submissionResult === "complete") return "complete";
  if (submissionResult === "error") return "error";
  if (!hasToken) return "missing-token";
  if (!hasSession) return "authenticating";
  return "submitting";
}

function getAckToken(): string | null {
  const hashParams = new URLSearchParams(
    window.location.hash.replace(/^#/, ""),
  );
  return hashParams.get("ack_token") ?? hashParams.get("token");
}

function AcknowledgeMessage({
  state,
  isPending,
  onRetry,
  onDecline,
}: {
  state: AcknowledgeState;
  isPending: boolean;
  onRetry: () => void;
  onDecline: () => void;
}) {
  if (state === "complete") {
    return (
      <Stack gap={3} align="center">
        <div className="bg-primary/10 flex h-11 w-11 items-center justify-center rounded-full">
          <Icon name="check" className="text-primary h-5 w-5" />
        </div>
        <Stack gap={1} align="center">
          <Type variant="subheading" className="text-center">
            Acknowledged
          </Type>
          <Type muted small className="text-center">
            Return to your agent and retry the request. You can close this page.
          </Type>
        </Stack>
      </Stack>
    );
  }

  if (state === "declined") {
    return (
      <Stack gap={3} align="center">
        <div className="bg-muted flex h-11 w-11 items-center justify-center rounded-full">
          <Icon name="x" className="text-muted-foreground h-5 w-5" />
        </div>
        <Stack gap={1} align="center">
          <Type variant="subheading" className="text-center">
            Declined
          </Type>
          <Type muted small className="text-center">
            Nothing was acknowledged. The request remains blocked. You can close
            this page.
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
            This acknowledgement link is no longer valid. Try the action again
            in your agent to generate a new one.
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
            Acknowledgement failed
          </Type>
          <Type muted small className="text-center">
            We could not record your acknowledgement. Check your connection and
            try again.
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
        {isPending ? "Recording acknowledgement..." : "Preparing..."}
      </Type>
      <Button variant="tertiary" onClick={onDecline} disabled={isPending}>
        <Button.Text>Decline</Button.Text>
      </Button>
    </Stack>
  );
}
