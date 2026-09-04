import { FormPage } from "@/components/page-templates";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam, validateMcpServerUrl } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Stack } from "@/components/ui/Stack";
import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import { AlertCircle, Loader2, Plug } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useCreateRemoteMcpSource } from "./hooks";
import { useCreateUnproxiedMcpSource } from "../unproxied-mcp/hooks";
import { useVerifyRemoteMcpUrl } from "./useVerifyRemoteMcpUrl";
import { VerifyRemoteMcpUrlAlert } from "./VerifyRemoteMcpUrlButton";

// Both backends are, to the administrator, the same thing: a server that lives
// at a URL somewhere else. The only difference is whether Gram sits in the
// request path, so that is the one question the form asks — and only of staff,
// since unproxied servers are staff-only today.
type ProxyMode = "proxied" | "unproxied";

export default function CreateRemoteMcp(): JSX.Element {
  return <CreateRemoteMcpForm />;
}

function CreateRemoteMcpForm() {
  const routes = useRoutes();
  const isSpeakeasyStaff = useIsSpeakeasyStaff();
  const createRemote = useCreateRemoteMcpSource();
  const createUnproxied = useCreateUnproxiedMcpSource();

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [mode, setMode] = useState<ProxyMode>("proxied");
  // Track whether the field has been touched so we don't surface "URL is
  // required" the moment the page renders.
  const [touched, setTouched] = useState(false);

  const verify = useVerifyRemoteMcpUrl(url);

  const isPending = createRemote.isPending || createUnproxied.isPending;
  const createError = createRemote.error ?? createUnproxied.error;
  const isCreateError = createRemote.isError || createUnproxied.isError;

  const validationError = touched ? validateMcpServerUrl(url) : null;
  const urlUsable = !!url.trim() && validateMcpServerUrl(url) === null;
  // The verify result is cleared whenever the URL changes (see
  // useVerifyRemoteMcpUrl), so this can only be true for the URL on screen.
  const isVerified = verify.result?.verified === true;

  const handleVerify = () => {
    setTouched(true);
    if (!urlUsable) return;
    void verify.trigger();
  };

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (!urlUsable) return;
    // Connectivity is part of saving rather than a side errand: an unverified
    // URL falls through to a verify instead of creating a server nobody can
    // reach.
    if (!isVerified) {
      void verify.trigger();
      return;
    }

    const trimmedName = name.trim();
    try {
      if (mode === "unproxied") {
        const { mcpServer } = await createUnproxied.mutateAsync({
          name: trimmedName === "" ? undefined : trimmedName,
          url: url.trim(),
        });
        toast.success("MCP server added");
        routes.mcp.x.overview.goTo(mcpServerRouteParam(mcpServer));
        return;
      }

      const { authAutoConfig, mcpServer } = await createRemote.mutateAsync({
        name: trimmedName === "" ? undefined : trimmedName,
        url: url.trim(),
      });
      if (authAutoConfig.status === "configured") {
        toast.success("MCP server added and authentication configured");
      } else {
        toast.success("MCP server added");
        if (authAutoConfig.warn) {
          toast.warning(authAutoConfig.message);
        }
      }
      routes.mcp.x.overview.goTo(mcpServerRouteParam(mcpServer));
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to add MCP server";
      toast.error(message);
    }
  };

  return (
    <FormPage
      scope="mcp:write"
      title="New remote MCP server"
      description="Register a server that already runs somewhere else by its URL."
    >
      <form
        onSubmit={(e) => {
          void handleSubmit(e);
        }}
        noValidate
      >
        <Stack gap={4}>
          <Stack gap={1}>
            <label
              htmlFor="remote-mcp-name"
              className="text-sm leading-none font-medium"
            >
              Display name (optional)
            </label>
            <Input
              id="remote-mcp-name"
              autoFocus
              placeholder="My MCP server"
              value={name}
              onChange={setName}
            />
          </Stack>

          <Stack gap={1}>
            <label
              htmlFor="remote-mcp-url"
              className="text-sm leading-none font-medium"
            >
              MCP server URL
            </label>
            <Input
              id="remote-mcp-url"
              placeholder="https://example.com/mcp"
              value={url}
              onChange={(value) => {
                setUrl(value);
                if (!touched) setTouched(true);
              }}
              onBlur={() => setTouched(true)}
              aria-invalid={validationError ? true : undefined}
              aria-describedby={
                validationError ? "remote-mcp-url-error" : undefined
              }
            />
            {validationError && (
              <div
                id="remote-mcp-url-error"
                role="alert"
                className="text-destructive mt-2 flex items-center gap-1.5 text-xs"
              >
                <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                <span>{validationError}</span>
              </div>
            )}
            <VerifyRemoteMcpUrlAlert state={verify} />
          </Stack>

          {isSpeakeasyStaff && (
            <Stack gap={2}>
              <label className="text-sm leading-none font-medium">
                Connection
              </label>
              <RadioGroup
                value={mode}
                onValueChange={(value) => setMode(value as ProxyMode)}
              >
                <label className="flex items-start gap-2.5">
                  <RadioGroupItem value="proxied" className="mt-0.5" />
                  <span className="flex flex-col gap-0.5">
                    <span className="text-sm">Proxy through Speakeasy</span>
                    <Text muted small>
                      Requests route through us over streamable-http, so we can
                      manage authentication and see the traffic.
                    </Text>
                  </span>
                </label>
                <label className="flex items-start gap-2.5">
                  <RadioGroupItem value="unproxied" className="mt-0.5" />
                  <span className="flex flex-col gap-0.5">
                    <span className="text-sm">
                      Clients connect directly{" "}
                      <span className="text-muted-foreground">
                        (Speakeasy staff only)
                      </span>
                    </span>
                    <Text muted small>
                      We list the server but never sit in the request path, so
                      the vendor&apos;s own OAuth applies. Use this to sidestep
                      per-vendor callback allowlisting.
                    </Text>
                  </span>
                </label>
              </RadioGroup>
            </Stack>
          )}

          {isCreateError && createError && (
            <Alert variant="error" dismissible={false}>
              {createError.message}
            </Alert>
          )}

          <Stack direction="horizontal" gap={2}>
            {/* One primary action that advances through the flow: verify, then
                add. A server that cannot be reached is never worth saving, so
                the two steps are the same button rather than a check the user
                can skip. */}
            <Button
              type="submit"
              variant="primary"
              disabled={!urlUsable || verify.isPending || isPending}
            >
              {verify.isPending || isPending ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : !isVerified ? (
                <Button.LeftIcon>
                  <Plug className="size-4" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {verify.isPending
                  ? "Verifying"
                  : isPending
                    ? "Saving"
                    : isVerified
                      ? "Save"
                      : "Verify connectivity"}
              </Button.Text>
            </Button>
            {isVerified && (
              <Button
                type="button"
                variant="secondary"
                disabled={verify.isPending || isPending}
                onClick={handleVerify}
              >
                <Button.Text>Re-verify</Button.Text>
              </Button>
            )}
            <Button
              type="button"
              variant="secondary"
              disabled={isPending}
              onClick={() => routes.mcp.add.goTo()}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </form>
    </FormPage>
  );
}
