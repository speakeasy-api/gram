import { cn, getServerURL } from "@/lib/utils";

export const AssetImage = ({
  assetId,
  className,
}: {
  assetId: string;
  className?: string;
}) => {
  return (
    <img
      src={`${getServerURL()}/rpc/assets.serveImage?id=${assetId}`}
      alt={"Uploaded image"}
      className={cn("w-[200px] h-[200px] rounded-lg", className)}
    />
  );
};
