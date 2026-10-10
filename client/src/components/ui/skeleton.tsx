export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      className={`animate-pulse rounded-lg bg-secondary ${className ?? ""}`}
      aria-hidden="true"
    />
  );
}

const skeletonRowKeys = [
  "row-1",
  "row-2",
  "row-3",
  "row-4",
  "row-5",
  "row-6",
  "row-7",
  "row-8",
];

export function SkeletonList({ rows = 4 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-3">
      {skeletonRowKeys.slice(0, rows).map((key) => (
        <div key={key} className="rounded-xl border border-border bg-card p-4">
          <div className="flex gap-4">
            <Skeleton className="h-16 w-24 shrink-0" />
            <div className="min-w-0 flex-1">
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="mt-2 h-3 w-1/2" />
              <Skeleton className="mt-2 h-3 w-2/3" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
