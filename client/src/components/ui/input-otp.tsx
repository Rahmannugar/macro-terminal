import { OTPInput, OTPInputContext } from "input-otp";
import { type ComponentPropsWithoutRef, type ReactNode, useContext } from "react";

export function InputOTP(props: ComponentPropsWithoutRef<typeof OTPInput>) {
  const { containerClassName, ...rest } = props;
  return (
    <OTPInput
      containerClassName={`flex items-center has-disabled:opacity-50 ${containerClassName ?? ""}`}
      {...rest}
    />
  );
}

export function InputOTPGroup({ children }: { children: ReactNode }) {
  return <div className="flex items-center gap-2">{children}</div>;
}

export function InputOTPSlot({ index, className }: { index: number; className?: string }) {
  const context = useContext(OTPInputContext);
  const slot = context.slots[index];
  const stateClassName = slot?.isActive
    ? "border-primary ring-4 ring-primary/15"
    : "border-border";
  return (
    <div
      className={`relative flex h-12 w-10 items-center justify-center rounded-lg border bg-card text-lg font-semibold text-foreground ${stateClassName} ${className ?? ""}`}
    >
      {slot?.char ?? ""}
      {slot?.hasFakeCaret ? (
        <span aria-hidden="true" className="absolute h-5 w-px animate-pulse bg-foreground" />
      ) : null}
    </div>
  );
}
