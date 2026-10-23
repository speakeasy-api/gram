import { TabbedPage } from "@/components/page-templates";
import { useSearchParams } from "react-router";
import { SensorsTab } from "./SensorsTab";
import { SignalsTab } from "./SignalsTab";
import { SignalExplore } from "./SignalExplore";

export default function SignalsIntelligence(): JSX.Element {
  const [searchParams] = useSearchParams();
  const tab = searchParams.get("tab");
  const activeTab = tab === "signals" || tab === "sensors" ? tab : "explore";

  return (
    <TabbedPage
      title="Signals intelligence"
      description="Explore patterns in agent conversations and the signals that measure them."
      stage="preview"
      activeTab={activeTab}
      tabs={[
        { value: "explore", label: "Explore", href: "?tab=explore" },
        { value: "sensors", label: "Sensors", href: "?tab=sensors" },
        { value: "signals", label: "Signals", href: "?tab=signals" },
      ]}
    >
      {activeTab === "explore" && <SignalExplore />}
      {activeTab === "sensors" && <SensorsTab />}
      {activeTab === "signals" && <SignalsTab />}
    </TabbedPage>
  );
}
