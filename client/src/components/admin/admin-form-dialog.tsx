import type { FormEvent, ReactNode } from "react";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";

export function AdminFormDialog({
  open,
  onClose,
  title,
  description,
  pending,
  errorMessage,
  submitLabel = "Create",
  onSubmit,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  pending: boolean;
  errorMessage?: string | null;
  submitLabel?: string;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  children: ReactNode;
}) {
  return (
    <Dialog open={open} onClose={onClose} title={title} description={description}>
      <form
        className="mt-5 flex flex-col gap-4"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          if (pending) return;
          onSubmit(event);
        }}
      >
        {children}
        {errorMessage ? (
          <p
            role="alert"
            className="rounded-lg bg-red-500/10 px-3.5 py-2.5 text-sm text-red-600"
          >
            {errorMessage}
          </p>
        ) : null}
        <div className="flex justify-end gap-2 pt-1">
          <Button
            type="button"
            variant="quiet"
            className="h-10 px-4"
            disabled={pending}
            onClick={onClose}
          >
            Cancel
          </Button>
          <Button type="submit" className="h-10 px-4" pending={pending}>
            {submitLabel}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
