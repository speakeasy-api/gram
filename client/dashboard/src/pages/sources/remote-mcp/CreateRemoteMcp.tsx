import { FormPage } from "@/components/page-templates";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam, validateMcpServerUrl } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { AlertCircle, Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useCreateRemoteMcpSource } from "./hooks";
import { useVerifyRemoteMcpUrl } from "./useVerifyRemoteMcpUrl";
import {
  VerifyRemoteMcpUrlAlert,
  VerifyRemoteMcpUrlButton,
} from "./VerifyRemoteMcpUrlButton";

export default function CreateRemoteMcp(): JSX.Element {
  return <CreateRemoteMcpForm />;
}

function CreateRemoteMcpForm() {
  const routes = useRoutes();
  const createSource = useCreateRemoteMcpSource();

  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  // Track whether the field has been touched so we don't surface "URL is
  // required" the moment the page renders.
  const [touched, setTouched] = useState(false);

  const verify = useVerifyRemoteMcpUrl(url);

  const validationError = touched ? validateMcpServerUrl(url) : null;
  const submitDisabled =
    createSource.isPending || !url.trim() || validateMcpServerUrl(url) !== null;
  const verifyDisabled =
    createSource.isPending || !url.trim() || validateMcpServerUrl(url) !== null;

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (validateMcpServerUrl(url) !== null) {
      return;
    }
    try {
      const trimmedName = name.trim();
      const { authAutoConfig, mcpServer } = await createSource.mutateAsync({
        name: trimmedName === "" ? undefined : trimmedName,
        url: url.trim(),
      });
      if (authAutoConfig.status === "configured") {
        toast.success("Remote MCP server added and authentication configured");
      } else {
        toast.success("Remote MCP server added");
        if (authAutoConfig.warn) {
          toast.warning(authAutoConfig.message);
        }
      }
      routes.mcp.x.overview.goTo(mcpServerRouteParam(mcpServer));
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to add remote MCP server";
      toast.error(message);
    }
  };

  return (
    <FormPage
      scope="mcp:write"
      title="New remote MCP server"
      description="Register an existing remote MCP server by URL. We'll proxy requests to it using streamable-http transport."
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
              Remote MCP server URL
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

          <Stack gap={1}>
            <label className="text-sm leading-none font-medium">
              Transport
            </label>
            <Text muted small>
              streamable-http
            </Text>
          </Stack>

          {createSource.isError && (
            <Alert variant="error" dismissible={false}>
              {createSource.error.message}
            </Alert>
          )}

          <Stack direction="horizontal" gap={2}>
            <VerifyRemoteMcpUrlButton
              state={verify}
              url={url}
              disabled={verifyDisabled}
            />
            <Button type="submit" variant="primary" disabled={submitDisabled}>
              {createSource.isPending ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>
                {createSource.isPending ? "Adding" : "Add server"}
              </Button.Text>
            </Button>
            <Button
              type="button"
              variant="secondary"
              disabled={createSource.isPending}
              onClick={() => routes.sources.goTo()}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </Stack>
        </Stack>
      </form>
    </FormPage>
  );
}
