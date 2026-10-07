import { Link } from "react-router";
import { formatTimestamp, plainSnippet } from "../lib/format";
import type { ArticleSummary } from "../lib/terminal";
import { Button } from "./ui/button";

type ArticleListProps = {
  articles: ArticleSummary[];
  linkState?: { from: string };
  loading?: boolean;
  loadingMore?: boolean;
  hasNext?: boolean;
  error?: unknown;
  onRetry?: () => void;
  onLoadMore?: () => void;
  emptyLabel?: string;
};

export function ArticleList({
  articles,
  linkState,
  loading,
  loadingMore,
  hasNext,
  error,
  onRetry,
  onLoadMore,
  emptyLabel = "Nothing here yet.",
}: ArticleListProps) {
  if (loading) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }
  if (error) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center">
        <p className="text-sm text-muted-foreground">
          {error instanceof Error ? error.message : "The list could not be loaded."}
        </p>
        {onRetry ? (
          <Button variant="secondary" className="mt-4 h-9 px-4 text-sm" onClick={onRetry}>
            Retry
          </Button>
        ) : null}
      </div>
    );
  }
  if (articles.length === 0) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
        {emptyLabel}
      </div>
    );
  }

  return (
    <div>
      <ul className="flex flex-col gap-3">
        {articles.map((article) => (
          <li key={article.id}>
            <Link
              to={`/articles/${article.id}`}
              state={linkState}
              className="block rounded-xl border border-border bg-card p-4 transition-colors hover:border-primary/40 hover:bg-secondary/60"
            >
              <p className="text-sm font-semibold leading-6">{article.title}</p>
              <p className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                {article.source.name ? <span>{article.source.name}</span> : null}
                <span>{formatTimestamp(article.publishedAt)}</span>
              </p>
              {plainSnippet(article.content) ? (
                <p className="mt-2 line-clamp-2 text-sm leading-6 text-muted-foreground">
                  {plainSnippet(article.content)}
                </p>
              ) : null}
            </Link>
          </li>
        ))}
      </ul>
      {hasNext || loadingMore ? (
        <div className="mt-3 flex min-h-10 items-center justify-center">
          <Button
            variant="secondary"
            className="h-10 px-5 text-sm"
            pending={loadingMore}
            onClick={onLoadMore}
          >
            Load more
          </Button>
        </div>
      ) : (
        <p className="mt-3 text-center text-xs text-muted-foreground">End of list</p>
      )}
    </div>
  );
}
