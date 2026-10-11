import { ImageIcon } from "@phosphor-icons/react";
import { useState } from "react";

type ArticleImageProps = {
  src: string | null;
  alt: string;
  className?: string;
};

export function ArticleImage({ src, alt, className }: ArticleImageProps) {
  const [failed, setFailed] = useState(false);

  if (!src || failed) {
    return (
      <div
        aria-hidden="true"
        className={`flex items-center justify-center bg-secondary text-muted-foreground ${className ?? ""}`}
      >
        <ImageIcon className="size-1/3 max-h-8 max-w-8" />
      </div>
    );
  }

  return (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
      className={className}
    />
  );
}
