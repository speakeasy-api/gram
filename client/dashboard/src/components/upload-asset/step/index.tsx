import Content from "./content";
import { Provider, useContext } from "./context";
import { Header } from "./header";
import Frame from "./frame";
import Indicator from "./indicator";

type RootProps = {
  children: React.ReactNode;
  step: number;
};

function Root({ children, step }: RootProps) {
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
