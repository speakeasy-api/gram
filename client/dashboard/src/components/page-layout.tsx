import { PageHeader } from "./page-header.tsx";

function PageLayout({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

function PageBody({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-1 flex-col">
      <div className="@container/main flex flex-1 flex-col gap-2">
        <div className="flex flex-col gap-4 p-8 md:gap-6 md:py-6">
          {children}
        </div>
      </div>
    </div>
  );
}

export const Page = Object.assign(PageLayout, {
  Header: PageHeader,
  Body: PageBody,
});
