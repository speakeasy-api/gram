import { Download, IdCard } from "lucide-react";
import { ManagedProfileExample } from "@/pages/device-agent/device-agent-setup";

// The two things every MDM rollout has to deliver, side by side above the
// per-provider guides: the installer that puts the agent on the machine, and
// the managed profile that tells Speakeasy who is on it. The provider guides
// below differ only in how each MDM ships these two.
export function MdmRolloutRequirements(): JSX.Element {
  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <section className="border-border bg-card flex flex-col gap-3 border p-4">
          <div className="flex items-center gap-2">
            <Download className="text-muted-foreground h-4 w-4 flex-shrink-0" />
            <h4 className="text-foreground text-sm font-semibold">
              Installer rollout
            </h4>
          </div>
          <p className="text-muted-foreground text-sm leading-relaxed">
            For each platform you're targeting, the installer sets up the
            Speakeasy agent on your users' machines. The agent manages the AI
            clients on the machine and enforces the use of the observability
            tooling.
          </p>
        </section>

        <section className="border-border bg-card flex flex-col gap-3 border p-4">
          <div className="flex items-center gap-2">
            <IdCard className="text-muted-foreground h-4 w-4 flex-shrink-0" />
            <h4 className="text-foreground text-sm font-semibold">
              Managed profile
            </h4>
          </div>
          <p className="text-muted-foreground text-sm leading-relaxed">
            An admin-managed identity that lets Speakeasy know who is
            connecting. Deploy it alongside the installer so nobody enrolls by
            hand. Its template is below.
          </p>
        </section>
      </div>

      {/* The profile belongs to the card above, but a half-width column wraps
          the JSON mid-token, so the template gets the full column. */}
      <div className="border-border bg-card border p-4">
        <p className="text-eyebrow mb-3">Example managed.json</p>
        <ManagedProfileExample />
      </div>
    </div>
  );
}
