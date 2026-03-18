import { Page } from "@/components/page-layout";
import { ButtonRainbow } from "@/components/ui/button-rainbow";
import { Type } from "@/components/ui/type";
import { TerminalSquare } from "lucide-react";

export default function CLIs() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="flex flex-col items-center justify-center py-24">
          <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-4">
            <TerminalSquare className="w-8 h-8 text-muted-foreground" />
          </div>
          <Type variant="subheading" className="mb-2">
            Coming Soon
          </Type>
          <Type muted className="max-w-md text-center">
            Build and distribute CLI tools for your API sources. Secure with
            OAuth and track usage alongside your MCP insights and logs.
          </Type>
          <ButtonRainbow
            href="https://www.speakeasy.com/book-demo"
            className="mt-6"
          >
            Book a Demo
          </ButtonRainbow>
        </div>
      </Page.Body>
    </Page>
  );
}
