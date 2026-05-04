import { createFileRoute, notFound } from "@tanstack/react-router";
import { match } from "ts-pattern";
import { LocalModePane } from "@/components/providers/LocalModePane";
import { WorkosModePane } from "@/components/providers/WorkosModePane";
import { CapabilitiesCard } from "@/components/providers/CapabilitiesCard";
import { ActivationCard } from "@/components/providers/ActivationCard";
import { MODES, type Mode } from "@/lib/devidp";
import { isActivatable } from "@/lib/provider-info";

function isMode(s: string): s is Mode {
  return (MODES as readonly string[]).includes(s);
}

export const Route = createFileRoute("/providers/$mode")({
  beforeLoad: ({ params }) => {
    if (!isMode(params.mode)) throw notFound();
  },
  component: ProviderModePage,
});

function ProviderModePage() {
  const { mode } = Route.useParams();
  if (!isMode(mode)) return null;

  return (
    <div className="space-y-6">
      <div className="flex items-start gap-4">
        <div className="flex-1 min-w-0">
          <CapabilitiesCard mode={mode} />
        </div>
        {isActivatable(mode) && <ActivationCard mode={mode} />}
      </div>
      {match(mode)
        .with("workos", () => <WorkosModePane />)
        .otherwise((local) => (
          <LocalModePane mode={local} />
        ))}
    </div>
  );
}
