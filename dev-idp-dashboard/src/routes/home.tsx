import { createFileRoute } from "@tanstack/react-router";
import { ActiveModeCard } from "@/components/ActiveModeCard";
import { EnvReadout } from "@/components/EnvReadout";
import { HomeTab } from "@/components/HomeTab";

export const Route = createFileRoute("/home")({
  component: HomePage,
});

function HomePage() {
  return (
    <div className="max-w-6xl mx-auto space-y-6">
      <div className="grid grid-cols-1 lg:grid-cols-[1fr_24rem] gap-4 items-start">
        <ActiveModeCard />
        <EnvReadout />
      </div>
      <HomeTab />
    </div>
  );
}
