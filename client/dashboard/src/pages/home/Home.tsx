import { Page } from "@/components/page-layout";

export default function Home() {
  return (
    <Page>
      <Page.Header> 
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>Home Page Content</Page.Body>
    </Page>
  );
}
