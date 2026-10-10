import type { ReactNode } from "react";

const toneClasses: Record<string, string> = {
  neutral: "bg-secondary text-foreground",
  primary: "bg-primary/10 text-primary",
  success: "bg-emerald-500/10 text-emerald-600",
  danger: "bg-red-500/10 text-red-600",
  warning: "bg-orange-500/10 text-orange-600",
  caution: "bg-yellow-500/10 text-yellow-700 dark:text-yellow-500",
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
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${toneClasses[tone]}`}
    >
      {children}
    </span>
  );
}
