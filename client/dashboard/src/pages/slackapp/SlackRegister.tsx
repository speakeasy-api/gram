import { Type } from "@/components/ui/type";
import { getServerURL } from "@/lib/utils";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router";

type AppInfo = {
  appName: string;
  toolsets: { name: string; slug: string }[];
  token: string;
};

type PageState = "loading" | "ready" | "registering" | "complete" | "error";

export default function SlackRegister() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get("token");
  const [state, setState] = useState<PageState>("loading");
  const [appInfo, setAppInfo] = useState<AppInfo | null>(null);
  const [errorMessage, setErrorMessage] = useState("");

  useEffect(() => {
    if (!token) {
      setState("error");
      setErrorMessage("No token provided.");
      return;
    }

    fetch(
      `${getServerURL()}/rpc/slack-apps.getByToken?token=${encodeURIComponent(token)}`,
      { credentials: "include" },
    )
      .then((res) => {
        if (!res.ok) {
          throw new Error("Link expired or invalid");
        }
        return res.json();
      })
      .then((data: AppInfo) => {
        setAppInfo(data);
        setState("ready");
      })
      .catch((err) => {
        setState("error");
        setErrorMessage(err.message || "Failed to load app info");
      });
  }, [token]);

  const register = useCallback(async () => {
    if (!token) return;
    setState("registering");

    try {
      const res = await fetch(`${getServerURL()}/rpc/slack-apps.register`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token }),
      });

      if (!res.ok) {
        throw new Error("Failed to complete registration");
      }

      setState("complete");
    } catch (err) {
      setState("error");
      setErrorMessage(
        err instanceof Error ? err.message : "Something went wrong",
      );
    }
  }, [token]);

  return (
    <div className="bg-background flex min-h-screen items-center justify-center p-4">
      <div className="bg-card w-full max-w-md rounded-xl border p-8 shadow-sm">
        <Stack gap={6} align="center">
          <div className="bg-muted/50 flex h-12 w-12 items-center justify-center rounded-full">
            <Icon name="slack" className="text-muted-foreground h-6 w-6" />
          </div>

          {state === "loading" && (
            <Stack gap={2} align="center">
              <Icon
                name="loader-circle"
                className="text-muted-foreground h-6 w-6 animate-spin"
              />
              <Type muted small>
                Loading...
              </Type>
            </Stack>
          )}

          {state === "registering" && (
            <Stack gap={2} align="center">
              <Icon
                name="loader-circle"
                className="text-muted-foreground h-6 w-6 animate-spin"
              />
              <Type muted small>
                Linking your account...
              </Type>
            </Stack>
          )}

          {state === "ready" && appInfo && (
            <Stack gap={4} align="center" className="w-full">
              <Stack gap={1} align="center">
                <Type variant="subheading">Connect to Gram</Type>
                <Type muted small className="text-center">
                  <strong>{appInfo.appName}</strong> needs to verify your
                  identity to process your messages.
                </Type>
              </Stack>

              {appInfo.toolsets.length > 0 && (
                <div className="bg-muted/20 w-full rounded-lg border p-3">
                  <Type muted small className="mb-2 block font-medium">
                    MCP Servers
                  </Type>
                  <Stack gap={1}>
                    {appInfo.toolsets.map((ts) => (
                      <div
                        key={ts.slug}
                        className="flex items-center gap-2 rounded-md px-2 py-1"
                      >
                        <Icon
                          name="network"
                          className="text-muted-foreground h-3.5 w-3.5"
                        />
                        <Type small>{ts.name}</Type>
                      </div>
                    ))}
                  </Stack>
                </div>
              )}

              <Button className="w-full" onClick={register}>
                <Button.Text>Connect</Button.Text>
              </Button>
            </Stack>
          )}

          {state === "complete" && (
            <Stack gap={3} align="center">
              <div className="bg-primary/10 flex h-10 w-10 items-center justify-center rounded-full">
                <Icon name="check" className="text-primary h-5 w-5" />
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
              <div className="bg-destructive/10 flex h-10 w-10 items-center justify-center rounded-full">
                <Icon name="circle-x" className="text-destructive h-5 w-5" />
              </div>
              <Stack gap={1} align="center">
                <Type variant="subheading">Link expired</Type>
                <Type muted small className="text-center">
                  {errorMessage ||
                    "This registration link is no longer valid. Send another message in Slack to get a new one."}
                </Type>
              </Stack>
            </Stack>
          )}
        </Stack>
      </div>
    </div>
  );
}
