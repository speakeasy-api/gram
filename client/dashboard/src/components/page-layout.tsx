import { PageHeader } from "./page-header.tsx";

function PageLayout({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

function PageBody({ children }: { children: React.ReactNode }) {
  return (
    <div className="@container/main flex flex-1 flex-col gap-4 p-8">
      {children}
    </div>
  );
}

export const Page = Object.assign(PageLayout, {
  Header: PageHeader,
  Body: PageBody,
});
