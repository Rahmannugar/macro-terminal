import { Link, useLocation, useParams } from "react-router";
import { ArticleImage } from "../components/article-image";
import { ArticleList } from "../components/article-list";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { adminErrorMessage } from "../lib/admin";
import { formatTimestamp, toParagraphs } from "../lib/format";
import { useArticle, useExplainArticle, useRelatedArticles } from "../lib/terminal";

export function ArticleRoute() {
  const { id = "" } = useParams();
  const location = useLocation();
  const state = location.state as { from?: unknown } | null;
  const from = typeof state?.from === "string" ? state.from : "/search";
  const article = useArticle(id);
  const related = useRelatedArticles(id);
  const explain = useExplainArticle();

  const linkState = { from: `${location.pathname}${location.search}` };

  if (article.isPending) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }
  if (article.isError || !article.data) {
    return (
      <div className="rounded-xl border border-border bg-card px-4 py-10 text-center">
        <p className="text-sm text-muted-foreground">
          {article.error?.status === 404
            ? "This article does not exist."
            : adminErrorMessage(article.error)}
        </p>
        <div className="mt-4 flex justify-center gap-2">
          <Link
            to={from}
            className="inline-flex h-9 items-center rounded-lg border border-border bg-card px-4 text-sm font-semibold transition-colors hover:bg-secondary"
          >
            Go back
          </Link>
          <Button
            variant="secondary"
            className="h-9 px-4 text-sm"
            onClick={() => article.refetch()}
          >
            Retry
          </Button>
        </div>
      </div>
    );
  }

  const data = article.data;
  const paragraphs = toParagraphs(data.content);
  const explained = explain.isSuccess && explain.variables === id;

  return (
    <div>
      <Link
        to={from}
        className="inline-block text-sm text-muted-foreground transition-colors hover:text-foreground"
      >
        ← Back
      </Link>

      <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
        <h1 className="max-w-[760px] font-display text-[26px] font-semibold leading-tight tracking-[-0.03em]">
          {data.title}
        </h1>
        {data.url ? (
          <a
            href={data.url}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex h-9 shrink-0 items-center rounded-lg border border-border bg-card px-4 text-sm font-semibold transition-colors hover:bg-secondary"
          >
            Read original
          </a>
        ) : null}
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        {data.source.name ? <Badge>{data.source.name}</Badge> : null}
        <span>{formatTimestamp(data.publishedAt)}</span>
      </div>

      {data.imageUrl ? (
        <div className="mt-6 max-w-[680px] overflow-hidden rounded-xl border border-border">
          <ArticleImage
            key={data.id}
            src={data.imageUrl}
            alt=""
            className="aspect-video w-full object-cover"
          />
        </div>
      ) : null}

      <div className="mt-6 max-w-[680px]">
        {paragraphs.length > 0 ? (
          <p className="whitespace-pre-wrap text-[15px] leading-7">{paragraphs.join("\n\n")}</p>
        ) : (
          <p className="text-sm text-muted-foreground">
            No text was captured for this article.
          </p>
        )}
      </div>

      <div className="mt-8 max-w-[680px] rounded-xl border border-border bg-card p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <p className="text-sm font-semibold">Explain this article</p>
            <p className="mt-0.5 text-xs text-muted-foreground">
              A one-shot AI breakdown of the story and its market context.
            </p>
          </div>
          <Button
            variant="secondary"
            pending={explain.isPending}
            onClick={() => explain.mutate(id)}
          >
            Explain
          </Button>
        </div>
        {explained ? (
          <div className="mt-4 rounded-lg border border-primary/20 bg-primary/5 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-primary">
              AI explanation
            </p>
            <p className="mt-2 whitespace-pre-line text-sm leading-7">{explain.data}</p>
          </div>
        ) : null}
        {explain.isError ? (
          <p className="mt-3 text-sm text-red-600">{adminErrorMessage(explain.error)}</p>
        ) : null}
      </div>

      <div className="mt-10 max-w-[680px]">
        <h2 className="text-sm font-semibold">Related coverage</h2>
        <div className="mt-3">
          <ArticleList
            articles={related.data ?? []}
            linkState={linkState}
            loading={related.isPending}
            error={related.error}
            onRetry={() => related.refetch()}
            emptyLabel="No related coverage yet."
          />
        </div>
      </div>
    </div>
  );
}
