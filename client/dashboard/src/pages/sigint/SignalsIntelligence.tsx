import { TabbedPage } from "@/components/page-templates";
import { useSearchParams } from "react-router";
import { SensorsTab } from "./SensorsTab";
import { SignalsTab } from "./SignalsTab";

export default function SignalsIntelligence(): JSX.Element {
  const [searchParams] = useSearchParams();
  const activeTab =
    searchParams.get("tab") === "signals" ? "signals" : "sensors";

  return (
    <TabbedPage
      title="Signals intelligence"
      description="Create reusable signals, organize them into sensors, and enable or pause evaluation."
      stage="preview"
      activeTab={activeTab}
      tabs={[
        { value: "sensors", label: "Sensors", href: "?tab=sensors" },
        { value: "signals", label: "Signals", href: "?tab=signals" },
      ]}
    >
      {activeTab === "sensors" ? <SensorsTab /> : <SignalsTab />}
    </TabbedPage>
  );
}
