import { Badge } from "@/components/ui/Badge";

// Its own file rather than a neighbor of the column list: a module that
// exports a component beside a non-component export breaks fast refresh.
export function SignalBadges({ signals }: { signals: string[] }): JSX.Element {
  return (
    <div className="flex flex-wrap gap-1">
      {signals.includes("running") && (
        <Badge variant="information">
          <Badge.Text>Running</Badge.Text>
        </Badge>
      )}
      {signals.includes("installed") && (
        <Badge variant="neutral">
          <Badge.Text>Installed</Badge.Text>
        </Badge>
      )}
    </div>
  );
}
