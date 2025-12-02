import { Fragment, useId } from "react";
import tunnel from "tunnel-rat";

const WebGL = tunnel();

export const WebGLIn = ({ children }: { children: React.ReactNode }) => {
  const id = useId();
  return (
    <WebGL.In>
      <Fragment key={id}>{children}</Fragment>
    </WebGL.In>
  );
};

export const WebGLOut = WebGL.Out as () => React.ReactNode;
