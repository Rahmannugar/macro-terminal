import { type FormEvent, useState } from "react";
import { useSearchParams } from "react-router";
import { AdminScreen } from "../components/admin/admin-screen";
import { ArticleList } from "../components/article-list";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { adminErrorMessage, adminRows, useAdminList } from "../lib/admin";
import type { ArticleSummary } from "../lib/terminal";

export function SearchRoute() {
  const [searchParams, setSearchParams] = useSearchParams();
  const query = (searchParams.get("q") ?? "").trim();
  const [draft, setDraft] = useState(query);
  const [lastQuery, setLastQuery] = useState(query);

  if (lastQuery !== query) {
    setLastQuery(query);
    setDraft(query);
  }

  const list = useAdminList<ArticleSummary>({
    key: ["search", "articles"],
    path: "/api/v1/search/articles",
    rowsKey: "articles",
    filters: { q: query },
    enabled: query.length > 0,
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    const next = draft.trim();
    setSearchParams(next ? { q: next } : {});
  }

  return (
    <AdminScreen
      title="Search"
      description="Semantic search across the article archive — results are ordered by meaning, not by date."
    >
      <form onSubmit={submit} className="flex max-w-[560px] gap-2">
        <Input
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder="e.g. Fed rate decision"
          aria-label="Search query"
        />
        <Button type="submit" className="shrink-0">
          Search
        </Button>
      </form>

      <div className="mt-6">
        {!query ? (
          <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
            Search the archive to read articles, inspect related coverage, and request one-shot
            AI explanations.
          </div>
        ) : list.isError ? (
          <div className="rounded-xl border border-border bg-card px-4 py-10 text-center">
            <p className="text-sm text-muted-foreground">{adminErrorMessage(list.error)}</p>
            <Button
              variant="secondary"
              className="mt-4 h-9 px-4 text-sm"
              onClick={() => list.refetch()}
            >
              Retry
            </Button>
          </div>
        ) : (
          <>
            <p className="mb-3 text-sm text-muted-foreground">
              {list.isPending ? "Searching…" : `Results for “${query}”`}
            </p>
            <ArticleList
              articles={adminRows(list)}
              linkState={{ from: `/search?q=${encodeURIComponent(query)}` }}
              loading={list.isPending}
              loadingMore={list.isFetchingNextPage}
              hasNext={Boolean(list.hasNextPage)}
              onRetry={() => list.refetch()}
              onLoadMore={() => list.fetchNextPage()}
              emptyLabel={`No articles matched “${query}”.`}
            />
          </>
        )}
      </div>
    </AdminScreen>
  );
}
