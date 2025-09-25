import { Provider, useContext } from "./context";

type RootProps = {
  children: React.ReactNode;
  step: number;
};

const Root: React.FC<RootProps> = ({ children, step }) => {
  return (
    <Provider step={step}>
      <div className="flex flex-col gap-y-8">{children}</div>
    </Provider>
  );
};

export default {
  useContext,
  Root,
};
