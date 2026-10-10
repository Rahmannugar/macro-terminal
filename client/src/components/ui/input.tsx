import { Eye, EyeSlash } from "@phosphor-icons/react";
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
        className="absolute inset-y-0 right-0 flex w-10 items-center justify-center rounded-lg text-muted-foreground transition outline-none hover:text-foreground focus-visible:ring-4 focus-visible:ring-primary/15"
      >
        {visible ? <EyeSlash size={16} /> : <Eye size={16} />}
      </button>
    </span>
  );
}
