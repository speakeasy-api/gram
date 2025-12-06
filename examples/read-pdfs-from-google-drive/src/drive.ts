import { drive } from "@googleapis/drive";

export interface DriveFile {
  id: string;
  name: string;
  size: number;
  modifiedTime: string;
  mimeType: string;
  webViewLink?: string;
}

export async function searchFiles(
  accessToken: string,
  query: string,
  folderId?: string,
  fileType?: string
): Promise<DriveFile[]> {
  const driveClient = drive({
    version: "v3",
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
  });

  // Build search query
  let searchQuery = `trashed=false`;

  // Add folder restriction if specified
  if (folderId) {
    searchQuery += ` and '${folderId}' in parents`;
  }

  // Add name search if query provided
  if (query.trim()) {
    searchQuery += ` and name contains '${query.replace(/'/g, "\\'")}' `;
  }

  if (fileType) {
    searchQuery += ` and mimeType='${fileType}'`;
  }

  const response = await driveClient.files.list({
    q: searchQuery,
    fields: "files(id,name,size,modifiedTime,mimeType,webViewLink)",
    orderBy: "modifiedTime desc",
    pageSize: 100,
  });

  return (response.data.files || []).map((file) => ({
    id: file.id!,
    name: file.name!,
    size: parseInt(file.size || "0"),
    modifiedTime: file.modifiedTime!,
    mimeType: file.mimeType!,
    webViewLink: file.webViewLink || undefined,
  }));
}

export async function downloadFile(
  accessToken: string,
  fileId: string
): Promise<Buffer> {
  const driveClient = drive({
    version: "v3",
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
  });

  const response = await driveClient.files.get(
    {
      fileId: fileId,
      alt: "media",
    },
    {
      responseType: "stream",
    }
  );

  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    response.data.on("data", (chunk: Buffer) => chunks.push(chunk));
    response.data.on("end", () => resolve(Buffer.concat(chunks)));
    response.data.on("error", reject);
  });
}
