import { useQuery } from "@tanstack/react-query";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

async function fetchProxied(prefix: string, suffix: string) {
  const res = await fetch(`/devidp${prefix}/.well-known/${suffix}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

export function DiscoveryPane({ prefix }: { prefix: string }) {
  const discovery = useQuery({
    queryKey: ["discovery", prefix, "openid-configuration"],
    queryFn: () => fetchProxied(prefix, "openid-configuration"),
  });
  const jwks = useQuery({
    queryKey: ["discovery", prefix, "jwks"],
    queryFn: () => fetchProxied(prefix, "jwks.json"),
  });

  return (
    <div className="grid grid-cols-2 gap-4">
      <DiscoveryCard
        title=".well-known/openid-configuration"
        data={discovery.data}
        error={discovery.error}
        loading={discovery.isLoading}
      />
      <DiscoveryCard
        title=".well-known/jwks.json"
        data={jwks.data}
        error={jwks.error}
        loading={jwks.isLoading}
      />
    </div>
  );
}

function DiscoveryCard({
  title,
  data,
  error,
  loading,
}: {
  title: string;
  data: unknown;
  error: unknown;
  loading: boolean;
}) {
  return (
    <Card size="sm" className="min-w-0">
      <CardHeader>
        <CardTitle className="font-mono text-xs">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {loading && (
          <div className="text-xs text-muted-foreground">Loading…</div>
        )}
        {error ? (
          <div className="text-xs text-destructive">
            {(error as Error).message}
          </div>
        ) : null}
        {data !== undefined && (
          <pre className="text-xs bg-muted rounded-2xl p-3 overflow-x-auto max-h-80">
            <code>{JSON.stringify(data, null, 2)}</code>
          </pre>
        )}
      </CardContent>
    </Card>
  );
}
