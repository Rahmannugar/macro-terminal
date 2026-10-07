import type { ReactNode } from "react";

const toneClasses: Record<string, string> = {
  neutral: "bg-secondary text-foreground",
  primary: "bg-primary/10 text-primary",
  success: "bg-emerald-500/10 text-emerald-600",
  danger: "bg-red-500/10 text-red-600",
};

export function Badge({
  tone = "neutral",
  children,
}: {
  tone?: keyof typeof toneClasses;
  children: ReactNode;
}) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium capitalize ${toneClasses[tone]}`}
    >
      {children}
    </span>
  );
}
