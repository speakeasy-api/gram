import { Heading } from "@speakeasy-api/moonshine";
import React from "react";

type HeaderProps = {
  children: React.ReactNode;
};

const Content: React.FC<HeaderProps> = ({ children }) => {
  return <div className="[grid-area=content]">{children}</div>;
};

export default Content;
