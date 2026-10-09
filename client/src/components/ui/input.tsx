import { type InputHTMLAttributes, useState } from "react";

export const inputClassName =
  "h-11 w-full rounded-lg border border-border bg-card px-3.5 text-sm text-foreground outline-none transition placeholder:text-muted-foreground focus-visible:border-primary focus-visible:ring-4 focus-visible:ring-primary/15 disabled:opacity-60";

export function Input(props: InputHTMLAttributes<HTMLInputElement>) {
  const { className, ...rest } = props;
  return (
    <input
      className={className ? `${inputClassName} ${className}` : inputClassName}
      {...rest}
    />
  );
}

export function PasswordInput(props: InputHTMLAttributes<HTMLInputElement>) {
  const { className, ...rest } = props;
  const [visible, setVisible] = useState(false);
  const fieldClassName = className
    ? `${inputClassName} pr-11 ${className}`
    : `${inputClassName} pr-11`;
  return (
    <span className="relative block">
      <input type={visible ? "text" : "password"} className={fieldClassName} {...rest} />
      <button
        type="button"
        aria-label={visible ? "Hide password" : "Show password"}
        onClick={() => setVisible((current) => !current)}
        className="absolute inset-y-0 right-0 flex w-10 items-center justify-center text-muted-foreground transition hover:text-foreground"
      >
        {visible ? (
          <svg
            aria-hidden="true"
            viewBox="0 0 16 16"
            className="size-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <path
              d="M6.4 6.4a2 2 0 0 0 2.8 2.8M4.6 4.7C3 5.8 1.7 7.4 1.3 8c.9 1.4 3.4 3.5 6.7 3.5 1 0 2-.2 2.8-.6M12.9 11c1-.9 1.6-2.1 1.8-3-.4-.6-1.5-2.1-3.7-3.1"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            <path d="m2.5 2.5 11 11" strokeLinecap="round" />
          </svg>
        ) : (
          <svg
            aria-hidden="true"
            viewBox="0 0 16 16"
            className="size-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <path
              d="M8 4.5C4.9 4.5 2.4 6.6 1.5 8c.9 1.4 3.4 3.5 6.5 3.5S13.6 9.4 14.5 8c-.9-1.4-3.4-3.5-6.5-3.5Z"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            <circle cx="8" cy="8" r="2" />
          </svg>
        )}
      </button>
    </span>
  );
}
