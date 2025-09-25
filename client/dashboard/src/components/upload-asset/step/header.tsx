import React from "react";

type HeaderProps = {
  title: string;
  description: string;
};

export const Header: React.FC<HeaderProps> = ({ title, description }) => {
  return (
    <div className="[grid-area=header]">
      <h2 className="text-2xl font-light capitalize">{title}</h2>
      {description && (
        <p className="mt-1 text-sm text-muted-foreground">{description}</p>
      )}
    </div>
  );
};
