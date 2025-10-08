import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useEffect, useState } from "react";

interface CliCallbackProps {
  localCallbackUrl: string;
}

function generateKeyName(): string {
  const timestamp = new Date().toISOString();

  return `CLI Key - ${timestamp}`;
}

const errNonLocalCallback = "Callback URL must be localhost or 127.0.0.1";

function isCallbackLocal(callbackUrl: string): boolean {
  try {
    const url = new URL(callbackUrl);
    const hostname = url.hostname.toLowerCase();

    return hostname === "localhost" || hostname === "127.0.0.1";
  } catch {
    return false;
  }
}

function keyRequest(scopes: string[]) {
  const name = generateKeyName();

  return { createKeyForm: { name, scopes } };
}

async function createProducerKey(
  mutateAsync: ReturnType<typeof useCreateAPIKeyMutation>["mutateAsync"],
): Promise<string> {
  const request = keyRequest(["producer"]);
  const result = await mutateAsync({ request });
  if (!result.key) throw new Error("No API key returned from server");

  return result.key;
}

async function transmitKey(callbackUrl: string, apiKey: string): Promise<void> {
  const url = new URL(callbackUrl);
  url.searchParams.set("api_key", apiKey);

  const smoothRedirectDelay = new Promise((r) => setTimeout(r, 200));
  await smoothRedirectDelay;

  window.location.replace(url.toString());
}

export default function CliCallback({ localCallbackUrl }: CliCallbackProps) {
  let isMounted = true;
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync, isPending } = useCreateAPIKeyMutation();

  if (!isCallbackLocal(localCallbackUrl)) {
    return <FailedScreen error={errNonLocalCallback} />;
  }

  useEffect(() => {
    (async function () {
      try {
        transmitKey(localCallbackUrl, await createProducerKey(mutateAsync));
      } catch (err) {
        if (isMounted) {
          setError(
            err instanceof Error ? err.message : "Failed to create API key",
          );
        }
      }
    })();

    return () => {
      isMounted = false;
    };
  }, [localCallbackUrl, mutateAsync]);

  if (error) return <FailedScreen error={error} />;
  else if (isPending) return <WaitScreen />;
  else return <SuccessScreen />;
}

function WaitScreen() {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <h1 className="text-2xl font-bold mb-2">Creating API Key...</h1>
        <p className="text-gray-600">Please wait</p>
      </div>
    </div>
  );
}

function SuccessScreen() {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <h1 className="text-2xl font-bold mb-2">Redirecting...</h1>
        <p className="text-gray-600">You will be redirected shortly</p>
      </div>
    </div>
  );
}

function FailedScreen({ error }: { error: string }) {
  return (
    <div className="flex items-center justify-center h-screen">
      <div className="text-center">
        <h1 className="text-2xl font-bold text-red-600 mb-2">Error</h1>
        <p className="text-gray-600">{error}</p>
      </div>
    </div>
  );
}
