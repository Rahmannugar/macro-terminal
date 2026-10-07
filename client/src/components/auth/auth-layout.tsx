import type { FormEvent, ReactNode } from "react";

type AuthLayoutProps = {
  title: string;
  subtitle?: string;
  errorMessage?: string | null;
  pending: boolean;
  submitLabel: string;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  children: ReactNode;
  footer?: ReactNode;
};

export function AuthLayout({
  title,
  subtitle,
  errorMessage,
  pending,
  submitLabel,
  onSubmit,
  children,
  footer,
}: AuthLayoutProps) {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center bg-background px-5 py-10">
      <div className="w-full max-w-[400px]">
        <img src="/macroterminal.png" alt="" width={56} height={56} className="rounded-xl" />
        <h1 className="mt-5 font-display text-[28px] font-semibold tracking-[-0.04em]">
          {title}
        </h1>
        {subtitle ? <p className="mt-2 text-sm text-muted-foreground">{subtitle}</p> : null}
        <form className="mt-7 flex flex-col gap-4" onSubmit={onSubmit} noValidate>
          {children}
          {errorMessage ? (
            <p
              role="alert"
              className="rounded-lg bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600 dark:text-red-400"
            >
              {errorMessage}
            </p>
          ) : null}
          <button
            type="submit"
            className="h-11 rounded-lg bg-primary px-4 text-sm font-semibold text-white transition outline-none hover:bg-[#076fd2] focus-visible:ring-4 focus-visible:ring-primary/20 disabled:pointer-events-none disabled:opacity-60"
            disabled={pending}
          >
            {pending ? "Working…" : submitLabel}
          </button>
        </form>
        {footer ? (
          <div className="mt-6 text-center text-sm text-muted-foreground">{footer}</div>
        ) : null}
      </div>
    </main>
  );
}

export function AuthField({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={htmlFor} className="text-sm font-medium text-foreground">
        {label}
      </label>
      {children}
    </div>
  );
}
