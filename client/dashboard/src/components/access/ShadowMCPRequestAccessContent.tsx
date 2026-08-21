import { FullScreenPage } from "@/components/full-screen-page";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { useRiskCreatePolicyBypassRequestMutation } from "@gram/client/react-query/riskCreatePolicyBypassRequest.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { TextArea } from "@/components/ui/Textarea";
import { useEffect, useState } from "react";

const REQUEST_TOKEN_STORAGE_KEY = "riskPolicyBypassRequestToken";
const NOTE_FIELD_ID = "request-access-note";
/**
 * What the endpoint accepts, counted the way it counts: Goa's MaxLength on a
 * string is runes, not UTF-16 units, so an emoji costs one here and one there.
 */
const NOTE_MAX_CHARACTERS = 4000;

function noteLength(note: string): number {
  return Array.from(note.trim()).length;
}
const LEGACY_REQUEST_TOKEN_STORAGE_KEY = "shadowMcpApprovalRequestToken";

type RequestAccessState =
  | "missing-token"
  | "authenticating"
  | "prompting"
  | "submitting"
  | "complete"
  | "error";

type SubmissionResult = "idle" | "submitting" | "complete" | "error";

export function ShadowMCPRequestAccessContent(): JSX.Element {
  const session = useSession();
  const requestToken = getRequestToken();
  const [submissionResult, setSubmissionResult] =
    useState<SubmissionResult>("idle");
  const [note, setNote] = useState("");
  const { mutateAsync: createApprovalRequest } =
    useRiskCreatePolicyBypassRequestMutation();

  // The request token is a bearer credential delivered in the URL fragment
  // (see GeneratePolicyBypassRequestURL on the server) so it never hits server
  // logs. Force no-referrer so it also can't leak via the Referer header to
  // anything this page loads.
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
      // Move the token out of the URL into sessionStorage and immediately strip
      // the fragment from the address bar / history, so it isn't left sitting
      // in a shared screen, browser history, or a copy-pasted URL.
      sessionStorage.setItem(REQUEST_TOKEN_STORAGE_KEY, requestToken);
      sessionStorage.removeItem(LEGACY_REQUEST_TOKEN_STORAGE_KEY);
      window.history.replaceState(null, "", window.location.pathname);
    }
  }, [requestToken]);

  const storedRequestToken =
    requestToken ??
    sessionStorage.getItem(REQUEST_TOKEN_STORAGE_KEY) ??
    sessionStorage.getItem(LEGACY_REQUEST_TOKEN_STORAGE_KEY);

  useEffect(() => {
    if (!storedRequestToken || session.session) return;

    window.location.href = buildLoginRedirectURL(window.location.pathname);
  }, [session.session, storedRequestToken]);

  // The ask is submitted from the button, not on arrival: the note is the
  // point of this page, and a page that files the request as it loads never
  // gives the requester anywhere to say what they need the server for.
  const submit = async () => {
    if (
      !storedRequestToken ||
      note.trim() === "" ||
      noteLength(note) > NOTE_MAX_CHARACTERS
    ) {
      return;
    }

    setSubmissionResult("submitting");
    try {
      await createApprovalRequest({
        request: {
          createRiskPolicyBypassRequestRequestBody: {
            requestToken: storedRequestToken,
            note: note.trim(),
          },
        },
      });
    } catch {
      setSubmissionResult("error");
      return;
    }

    setSubmissionResult("complete");
    sessionStorage.removeItem(REQUEST_TOKEN_STORAGE_KEY);
    sessionStorage.removeItem(LEGACY_REQUEST_TOKEN_STORAGE_KEY);
  };

  const state = getRequestAccessState({
    hasSession: !!session.session,
    hasToken: !!storedRequestToken,
    submissionResult,
  });

  return (
    <FullScreenPage>
      <RequestAccessMessage
        state={state}
        note={note}
        onNoteChange={setNote}
        onSubmit={() => void submit()}
        // A failed submit keeps what was typed: the note is the one thing
        // here the requester cannot get back from the link.
        onRetry={() => setSubmissionResult("idle")}
      />
    </FullScreenPage>
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
  if (submissionResult === "submitting") return "submitting";
  return "prompting";
}

function getRequestToken(): string | null {
  const hashParams = new URLSearchParams(
    window.location.hash.replace(/^#/, ""),
  );
  return hashParams.get("request_token") ?? hashParams.get("token");
}

function RequestAccessMessage({
  state,
  note,
  onNoteChange,
  onSubmit,
  onRetry,
}: {
  state: RequestAccessState;
  note: string;
  onNoteChange: (value: string) => void;
  onSubmit: () => void;
  onRetry: () => void;
}) {
  if (state === "prompting" || state === "submitting") {
    const pending = state === "submitting";
    const tooLong = noteLength(note) > NOTE_MAX_CHARACTERS;

    return (
      <Stack gap={4} className="w-full max-w-md">
        <Stack gap={1}>
          <Text variant="subheading">Request access</Text>
          <Text muted small>
            An admin decides whether this server is allowed. Tell them what you
            need it for — they see this alongside the evidence gathered about
            the server.
          </Text>
        </Stack>
        <Stack gap={1}>
          <label htmlFor={NOTE_FIELD_ID} className="text-sm font-medium">
            Why do you need this server?
          </label>
          <TextArea
            id={NOTE_FIELD_ID}
            value={note}
            onChange={onNoteChange}
            rows={4}
            placeholder="e.g. The docs team works in Notion and I need meeting notes searchable from the editor."
            className="resize-none text-sm"
          />
          {/* Said before the request is refused rather than after: the server
              rejects an over-long note with a generic failure, and the note is
              the one thing here that cannot be recovered from the link. */}
          {tooLong && (
            <Text muted small>
              {(noteLength(note) - NOTE_MAX_CHARACTERS).toLocaleString()}{" "}
              characters over the {NOTE_MAX_CHARACTERS.toLocaleString()}{" "}
              character limit.
            </Text>
          )}
        </Stack>
        <Button
          onClick={onSubmit}
          disabled={pending || note.trim() === "" || tooLong}
        >
          {pending && (
            <Button.LeftIcon>
              <Icon name="loader-circle" className="h-4 w-4 animate-spin" />
            </Button.LeftIcon>
          )}
          <Button.Text>{pending ? "Sending" : "Send request"}</Button.Text>
        </Button>
      </Stack>
    );
  }

  if (state === "complete") {
    return (
      <Stack gap={3} align="center">
        <Stack gap={1} align="center">
          <Text variant="subheading" className="text-center">
            Request sent
          </Text>
          <Text muted small className="text-center">
            You can close this page.
          </Text>
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
        <Text muted small className="text-center">
          Redirecting to sign in...
        </Text>
      </Stack>
    );
  }

  if (state === "missing-token") {
    return (
      <Stack gap={3} align="center">
        <Stack gap={1} align="center">
          <Text variant="subheading" className="text-center">
            Link expired
          </Text>
          <Text muted small className="text-center">
            This request link is no longer valid. Try the blocked MCP action
            again to generate a new request.
          </Text>
        </Stack>
      </Stack>
    );
  }

  if (state === "error") {
    return (
      <Stack gap={3} align="center">
        <Stack gap={1} align="center">
          <Text variant="subheading" className="text-center">
            Request failed
          </Text>
          <Text muted small className="text-center">
            We could not send this request. Check your connection and try again.
          </Text>
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
      <Text muted small className="text-center">
        Preparing request...
      </Text>
    </Stack>
  );
}
