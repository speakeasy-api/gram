import PDFParser from "pdf2json";
import * as drive from "./drive.ts";

export interface PDFContent {
  text: string;
  numPages: number;
  info: {
    Title?: string;
    Author?: string;
    Subject?: string;
    Creator?: string;
    Producer?: string;
    CreationDate?: string;
    ModificationDate?: string;
  };
}

export async function extractTextFromPDF(
  accessToken: string,
  fileId: string
): Promise<PDFContent> {
  // Download the PDF file from Google Drive
  const buffer = await drive.downloadFile(accessToken, fileId);

  return new Promise((resolve, reject) => {
    const pdfParser = new (PDFParser as any)(null, true);

    pdfParser.on("pdfParser_dataError", (errData: any) =>
      reject(new Error(errData.parserError))
    );

    pdfParser.on("pdfParser_dataReady", (pdfData: any) => {
      // Extract text from all pages
      const text = (pdfData.Pages || [])
        .map((page: any) => {
          return (page.Texts || [])
            .map((text: any) => {
              return decodeURIComponent(
                (text.R || []).map((r: any) => r.T).join("")
              );
            })
            .join(" ");
        })
        .join("\n\n");

      resolve({
        text,
        numPages: pdfData.Pages?.length || 0,
        info: {
          Title: pdfData.Meta?.Title,
          Author: pdfData.Meta?.Author,
          Subject: pdfData.Meta?.Subject,
          Creator: pdfData.Meta?.Creator,
          Producer: pdfData.Meta?.Producer,
          CreationDate: pdfData.Meta?.CreationDate,
          ModificationDate: pdfData.Meta?.ModDate,
        },
      });
    });

    pdfParser.parseBuffer(buffer);
  });
}
