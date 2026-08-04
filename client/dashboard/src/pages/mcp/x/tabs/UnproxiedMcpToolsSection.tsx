import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useUnproxiedMcpServerTools } from "@gram/client/react-query/unproxiedMcpServerTools.js";

type UnproxiedMcpToolsSectionProps = {
  unproxiedMcpServerId: string;
  /** Disabled servers cannot serve MCP requests, so skip the live probe. */
  isDisabled: boolean;
};

export function UnproxiedMcpToolsSection({
  unproxiedMcpServerId,
  isDisabled,
}: UnproxiedMcpToolsSectionProps): JSX.Element {
  const { data, isLoading } = useUnproxiedMcpServerTools(
    { id: unproxiedMcpServerId },
    undefined,
    { enabled: !isDisabled, throwOnError: false },
  );

  return (
    <div className="border-neutral-softest rounded-lg border">
      <div className="border-neutral-softest border-b px-4 py-3">
        <Text small muted>
          Discovered live from the vendor&apos;s server. Tool calls aren&apos;t
          proxied through Speakeasy.
        </Text>
      </div>
      {isDisabled ? (
        <div className="px-4 py-6">
          <Text small muted>
            Enable this server to load its tools.
          </Text>
        </div>
      ) : (
        <UnproxiedMcpToolsBody data={data} isLoading={isLoading} />
      )}
    </div>
  );
}

function UnproxiedMcpToolsBody({
  data,
  isLoading,
}: {
  data: ReturnType<typeof useUnproxiedMcpServerTools>["data"];
  isLoading: boolean;
}): JSX.Element {
  if (isLoading) {
    return (
      <div className="flex flex-col">
        {Array.from({ length: 3 }, (_, i) => (
          <div
            key={i}
            className="border-neutral-softest flex flex-col gap-2 border-b py-4 pr-3 pl-4 last:border-b-0"
          >
            <Skeleton className="h-4 w-40" />
            <Skeleton className="h-3 w-64" />
          </div>
        ))}
      </div>
    );
  }

  if (!data || data.status === "unreachable" || data.status === "error") {
    return (
      <div className="px-4 py-6">
        <Text small muted>
          {data?.message ?? "Could not reach the server."}
        </Text>
      </div>
    );
  }

  if (data.status === "auth_required") {
    return (
      <div className="px-4 py-6">
        <Text small muted>
          {data.message ??
            "This server requires authentication Speakeasy doesn't manage."}
        </Text>
      </div>
    );
  }

  if (data.tools.length === 0) {
    return (
      <div className="px-4 py-6">
        <Text small muted>
          The server didn&apos;t report any tools.
        </Text>
      </div>
    );
  }

  return (
    <div className="flex flex-col">
      {data.tools.map((tool) => (
        <div
          key={tool.name}
          className="border-neutral-softest flex flex-col border-b py-4 pr-3 pl-4 last:border-b-0"
        >
          <p className="text-foreground truncate text-sm leading-6">
            {tool.name}
          </p>
          <p className="text-muted-foreground truncate text-sm leading-6">
            {tool.description || "No description"}
          </p>
        </div>
      ))}
    </div>
  );
}
