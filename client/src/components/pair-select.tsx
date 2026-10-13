import { CaretDownIcon, MagnifyingGlassIcon } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import { adminErrorMessage } from "../lib/admin";
import { Input } from "./ui/input";
import { Skeleton } from "./ui/skeleton";

export type PairSelectOption = { id: string; symbol: string };

// PairSelect replaces the native <select> for pair picking: a button that
// opens a searchable list, sized to match the form controls around it.
export function PairSelect({
  value,
  options,
  onChange,
  disabled = false,
  loading = false,
  error,
  label,
}: {
  value: string | null;
  options: PairSelectOption[];
  onChange: (pairId: string) => void;
  disabled?: boolean;
  loading?: boolean;
  error?: unknown;
  label: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const containerRef = useRef<HTMLDivElement>(null);

  const selected = options.find((option) => option.id === value) ?? null;

  useEffect(() => {
    if (!open) return;
    function handlePointerDown(event: PointerEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  const search = query
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]/g, "");
  const filtered = options
    .filter(
      (option) =>
        !search ||
        option.symbol
          .toLowerCase()
          .replace(/[^a-z0-9]/g, "")
          .includes(search),
    )
    .toSorted((a, b) => a.symbol.localeCompare(b.symbol));

  return (
    <div ref={containerRef} className="relative w-full">
      <button
        type="button"
        aria-label={label}
        aria-expanded={open}
        aria-haspopup="listbox"
        disabled={disabled}
        onClick={() => {
          setQuery("");
          setOpen((current) => !current);
        }}
        className="flex h-11 w-full items-center justify-between gap-2 rounded-lg border border-border bg-card px-3.5 pr-3 text-sm text-foreground outline-none transition focus-visible:border-primary focus-visible:ring-4 focus-visible:ring-primary/15 disabled:opacity-60"
      >
        <span className="truncate font-medium">
          {selected ? (
            selected.symbol
          ) : loading ? (
            <Skeleton className="h-4 w-20" />
          ) : (
            "No pairs available"
          )}
        </span>
        <CaretDownIcon
          aria-hidden="true"
          className={`size-4 shrink-0 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>
      {open ? (
        <div className="absolute z-30 mt-1.5 w-full overflow-hidden rounded-lg border border-border bg-card shadow-lg">
          <div className="relative border-b border-border p-2">
            <MagnifyingGlassIcon
              aria-hidden="true"
              className="pointer-events-none absolute left-5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              autoFocus
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search pairs"
              aria-label="Search pairs"
              className="pl-8"
            />
          </div>
          <div className="max-h-64 overflow-y-auto" role="listbox" aria-label={label}>
            {loading ? (
              <div className="flex flex-col gap-2 p-2">
                {["row-1", "row-2", "row-3"].map((key) => (
                  <Skeleton key={key} className="h-9 rounded-md" />
                ))}
              </div>
            ) : error ? (
              <p className="p-3 text-sm text-muted-foreground">{adminErrorMessage(error)}</p>
            ) : filtered.length === 0 ? (
              <p className="p-3 text-sm text-muted-foreground">
                {search ? `No pairs matched “${query.trim()}”.` : "No pairs are available yet."}
              </p>
            ) : (
              <ul>
                {filtered.map((option) => (
                  <li key={option.id}>
                    <button
                      type="button"
                      role="option"
                      aria-selected={option.id === value}
                      className={`flex w-full items-center px-3.5 py-2.5 text-left text-sm transition-colors hover:bg-secondary ${
                        option.id === value ? "font-semibold text-primary" : "text-foreground"
                      }`}
                      onClick={() => {
                        onChange(option.id);
                        setOpen(false);
                      }}
                    >
                      {option.symbol}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
