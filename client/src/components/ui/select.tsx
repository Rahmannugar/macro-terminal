import type { SelectHTMLAttributes, TextareaHTMLAttributes } from "react";

export const selectClassName =
  "h-11 w-full appearance-none rounded-lg border border-border bg-card px-3.5 pr-9 text-sm text-foreground outline-none transition placeholder:text-muted-foreground focus-visible:border-primary focus-visible:ring-4 focus-visible:ring-primary/15 disabled:opacity-60";

export function Select({
  className,
  children,
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative">
      <select
        className={className ? `${selectClassName} ${className}` : selectClassName}
        {...rest}
      >
        {children}
      </select>
      <svg
        aria-hidden="true"
        viewBox="0 0 16 16"
        className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      >
        <path d="m4 6 4 4 4-4" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </div>
  );
}

export const textareaClassName =
  "w-full rounded-lg border border-border bg-card px-3.5 py-2.5 font-mono text-sm text-foreground outline-none transition placeholder:font-sans placeholder:text-muted-foreground focus-visible:border-primary focus-visible:ring-4 focus-visible:ring-primary/15 disabled:opacity-60";

export function Textarea({ className, ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={className ? `${textareaClassName} ${className}` : textareaClassName}
      {...rest}
    />
  );
}
