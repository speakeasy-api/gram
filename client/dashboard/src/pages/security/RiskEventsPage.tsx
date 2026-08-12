import { WorkbenchPage } from "@/components/page-templates";
import RiskEvents from "./RiskEvents";

export default function RiskEventsPage(): JSX.Element {
  return (
    <WorkbenchPage scope="org:admin">
      <RiskEvents />
    </WorkbenchPage>
  );
}
