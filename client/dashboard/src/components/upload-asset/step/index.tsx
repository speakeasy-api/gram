import Stepper from "../stepper";
import Content from "./content";
import { Provider, useContext } from "./context";
import Frame from "./frame";
import { Header } from "./header";
import Indicator from "./indicator";

type RootProps = {
  children: React.ReactNode;
  step: number;
};

function Root({ children, step }: RootProps) {
  const stepper = Stepper.useContext();

  stepper.registerStep(step);

  return (
    <Provider step={step}>
      <Frame>{children}</Frame>
    </Provider>
  );
}

export default {
  useContext,
  Root,
  Header,
  Indicator,
  Content,
};
