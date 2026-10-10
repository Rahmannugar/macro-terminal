import type { ButtonHTMLAttributes, ReactNode } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "quiet";
  pending?: boolean;
  children: ReactNode;
};

const variantClasses: Record<NonNullable<ButtonProps["variant"]>, string> = {
  primary:
    "border border-primary bg-primary text-white hover:bg-[#076fd2] focus-visible:ring-4 focus-visible:ring-primary/20",
  secondary:
    "border border-border bg-card text-foreground hover:bg-secondary focus-visible:ring-4 focus-visible:ring-primary/15",
  quiet:
    "border border-transparent bg-transparent text-muted-foreground hover:bg-secondary hover:text-foreground focus-visible:ring-4 focus-visible:ring-primary/15",
};

export function Button({
  variant = "primary",
  pending = false,
  className,
  disabled,
  children,
  type,
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type ?? "button"}
      className={`inline-flex h-11 items-center justify-center gap-2 rounded-lg px-4 text-sm font-semibold transition outline-none disabled:pointer-events-none disabled:opacity-60 ${variantClasses[variant]} ${className ?? ""}`}
      disabled={disabled || pending}
      {...rest}
    >
      {pending ? (
        <span
          aria-hidden="true"
          className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent"
        />
      ) : (
        children
      )}
    </button>
  );
}
