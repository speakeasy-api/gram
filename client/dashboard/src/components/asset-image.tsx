import { cn, getServerURL } from "@/lib/utils";

export const AssetImage = ({
  assetId,
  className,
  // Callers rendering the image as decoration beside visible text (listing
  // rows, detail titles, upload previews) should pass alt="" so screen
  // readers skip it instead of announcing the same generic string per image.
  alt = "Uploaded image",
}: {
  assetId: string;
  className?: string;
  alt?: string;
}): JSX.Element => {
  return (
    <img
      src={`${getServerURL()}/rpc/assets.serveImage?id=${assetId}`}
      alt={alt}
      className={cn("h-[200px] w-[200px]", className)}
    />
  );
};
