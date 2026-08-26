"use client";

import { cn } from "@/lib/utils";
import { type HTMLAttributes, useEffect, useRef, useState } from "react";
import type { BundledLanguage } from "shiki";
import { highlightCode } from "./highlight-code";

type CodeBlockProps = HTMLAttributes<HTMLDivElement> & {
  code: string;
  language: BundledLanguage;
  showLineNumbers?: boolean;
};

export const CodeBlock = ({
  code,
  language,
  showLineNumbers = false,
  className,
  children,
  ...props
}: CodeBlockProps): JSX.Element => {
  const [html, setHtml] = useState<string>("");
  const [darkHtml, setDarkHtml] = useState<string>("");
  const mounted = useRef(false);

  useEffect(() => {
    mounted.current = true;

    void highlightCode(code, language, showLineNumbers).then(
      ([light, dark]) => {
        if (mounted.current) {
          setHtml(light);
          setDarkHtml(dark);
        }
      },
    );

    return () => {
      mounted.current = false;
    };
  }, [code, language, showLineNumbers]);

  return (
    <div className="group relative">
      <div
        className={cn(
          "bg-background text-foreground w-full overflow-hidden border",
          className,
        )}
        {...props}
      >
        <div
          className="[&>pre]:bg-background! [&>pre]:text-foreground! overflow-x-auto dark:hidden [&_code]:font-mono [&_code]:text-sm [&>pre]:m-0 [&>pre]:p-4 [&>pre]:text-sm"
          // biome-ignore lint/security/noDangerouslySetInnerHtml: "this is needed."
          dangerouslySetInnerHTML={{ __html: html }}
        />
        <div
          className="[&>pre]:bg-background! [&>pre]:text-foreground! hidden overflow-x-auto dark:block [&_code]:font-mono [&_code]:text-sm [&>pre]:m-0 [&>pre]:p-4 [&>pre]:text-sm"
          // biome-ignore lint/security/noDangerouslySetInnerHtml: "this is needed."
          dangerouslySetInnerHTML={{ __html: darkHtml }}
        />
      </div>
      {children && (
        <div className="absolute top-2 right-2 z-10 flex items-center gap-2">
          {children}
        </div>
      )}
    </div>
  );
};
