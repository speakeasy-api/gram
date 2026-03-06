import { Type } from "@/components/ui/type";
import { getServerURL } from "@/lib/utils";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useCallback, useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router";

type AuthInfo = {
  appName: string;
  toolsets: { name: string; slug: string }[];
  token: string;
};

type PageState =
  | "loading"
  | "awaiting_login"
  | "completing"
  | "complete"
  | "error";

export default function SlackLogin() {
  const { token } = useParams<{ token: string }>();
  const navigate = useNavigate();
  const [state, setState] = useState<PageState>("loading");
  const [authInfo, setAuthInfo] = useState<AuthInfo | null>(null);
  const [errorMessage, setErrorMessage] = useState("");

  useEffect(() => {
    if (!token) {
      setState("error");
      setErrorMessage("No token provided.");
      return;
    }

    fetch(`${getServerURL()}/rpc/slack-apps/auth/${token}`, {
      credentials: "include",
    })
      .then((res) => {
        if (!res.ok) {
          throw new Error("Link expired or invalid");
        }
        return res.json();
      })
      .then((data: AuthInfo) => {
        setAuthInfo(data);
        setState("awaiting_login");
      })
      .catch((err) => {
        setState("error");
        setErrorMessage(err.message || "Failed to load auth info");
      });
  }, [token]);

  const completeAuth = useCallback(async () => {
    if (!token) return;
    setState("completing");

    try {
      const res = await fetch(
        `${getServerURL()}/rpc/slack-apps/auth/${token}/complete`,
        {
          method: "POST",
          credentials: "include",
        },
      );

      if (res.status === 401) {
        // Not logged in — redirect to login with redirect back here
        navigate(`/login?redirect=/slack/login/${token}`);
        return;
      }

      if (!res.ok) {
        throw new Error("Failed to complete authentication");
      }

      setState("complete");
    } catch (err) {
      setState("error");
      setErrorMessage(
        err instanceof Error ? err.message : "Something went wrong",
      );
    }
  }, [token, navigate]);

  // Auto-attempt completion on mount if user might already be logged in
  useEffect(() => {
    if (state === "awaiting_login") {
      // Try completing — if it fails with 401, we'll show the sign-in button
      completeAuth();
    }
  }, [state === "awaiting_login"]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <div className="w-full max-w-md rounded-xl border bg-card p-8 shadow-sm">
        <Stack gap={6} align="center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted/50">
            <Icon name="slack" className="h-6 w-6 text-muted-foreground" />
          </div>

          {state === "loading" && (
            <Stack gap={2} align="center">
              <Icon
                name="loader-circle"
                className="h-6 w-6 animate-spin text-muted-foreground"
              />
              <Type muted small>
                Loading...
              </Type>
            </Stack>
          )}

          {state === "completing" && (
            <Stack gap={2} align="center">
              <Icon
                name="loader-circle"
                className="h-6 w-6 animate-spin text-muted-foreground"
              />
              <Type muted small>
                Linking your account...
              </Type>
            </Stack>
          )}

          {state === "awaiting_login" && authInfo && (
            <Stack gap={4} align="center" className="w-full">
              <Stack gap={1} align="center">
                <Type variant="subheading">Sign in to Gram</Type>
                <Type muted small className="text-center">
                  <strong>{authInfo.appName}</strong> needs to verify your
                  identity to process your messages.
                </Type>
              </Stack>

              {authInfo.toolsets.length > 0 && (
                <div className="w-full rounded-lg border bg-muted/20 p-3">
                  <Type muted small className="mb-2 block font-medium">
                    MCP Servers
                  </Type>
                  <Stack gap={1}>
                    {authInfo.toolsets.map((ts) => (
                      <div
                        key={ts.slug}
                        className="flex items-center gap-2 rounded-md px-2 py-1"
                      >
                        <Icon
                          name="network"
                          className="h-3.5 w-3.5 text-muted-foreground"
                        />
                        <Type small>{ts.name}</Type>
                      </div>
                    ))}
                  </Stack>
                </div>
              )}

              <Button
                className="w-full"
                onClick={() =>
                  navigate(`/login?redirect=/slack/login/${token}`)
                }
              >
                <Button.Text>Sign in to Gram</Button.Text>
              </Button>
            </Stack>
          )}

          {state === "complete" && (
            <Stack gap={3} align="center">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-primary/10">
                <Icon name="check" className="h-5 w-5 text-primary" />
              </div>
              <Stack gap={1} align="center">
                <Type variant="subheading">You're connected!</Type>
                <Type muted small className="text-center">
                  You can close this page and return to Slack.
                </Type>
              </Stack>
            </Stack>
          )}

          {state === "error" && (
            <Stack gap={3} align="center">
              <div className="flex h-10 w-10 items-center justify-center rounded-full bg-destructive/10">
                <Icon name="circle-x" className="h-5 w-5 text-destructive" />
              </div>
              <Stack gap={1} align="center">
                <Type variant="subheading">Link expired</Type>
                <Type muted small className="text-center">
                  {errorMessage ||
                    "This login link is no longer valid. Send another message in Slack to get a new one."}
                </Type>
              </Stack>
            </Stack>
          )}
        </Stack>
      </div>
    </div>
  );
}
