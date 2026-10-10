import { type UseQueryResult, useMutation, useQuery } from "@tanstack/react-query";
import { APIError, apiRequest } from "./api";

export const explanationTimeoutMs = 35_000;

export type ArticleSummary = {
  id: string;
  title: string;
  content: string;
  url: string;
  imageUrl: string | null;
  publishedAt: string | null;
  source: { id: string; name: string };
};

function expectRecord(payload: unknown): Record<string, unknown> {
  if (typeof payload !== "object" || payload === null) {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected response.",
    );
  }
  return payload as Record<string, unknown>;
}

function parseArticle(value: unknown): ArticleSummary {
  const record = expectRecord(value);
  const source = expectRecord(record.source);
  if (typeof record.id !== "string" || typeof record.title !== "string") {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected article.",
    );
  }
  return {
    id: record.id,
    title: record.title,
    content: typeof record.content === "string" ? record.content : "",
    url: typeof record.url === "string" ? record.url : "",
    imageUrl:
      typeof record.imageUrl === "string" && record.imageUrl !== "" ? record.imageUrl : null,
    publishedAt: typeof record.publishedAt === "string" ? record.publishedAt : null,
    source: {
      id: typeof source.id === "string" ? source.id : "",
      name: typeof source.name === "string" ? source.name : "",
    },
  };
}

export function useArticle(id: string): UseQueryResult<ArticleSummary, APIError> {
  return useQuery<ArticleSummary, APIError>({
    queryKey: ["article", id],
    enabled: Boolean(id),
    staleTime: 60_000,
    retry: false,
    queryFn: async () => {
      const payload = await apiRequest(`/api/v1/articles/${id}`);
      return parseArticle(expectRecord(payload).article);
    },
  });
}

export function useRelatedArticles(id: string): UseQueryResult<ArticleSummary[], APIError> {
  return useQuery<ArticleSummary[], APIError>({
    queryKey: ["article", id, "related"],
    enabled: Boolean(id),
    staleTime: 60_000,
    retry: false,
    queryFn: async () => {
      const payload = await apiRequest(`/api/v1/articles/${id}/related?limit=25`);
      const articles = expectRecord(payload).articles;
      if (!Array.isArray(articles)) {
        throw new APIError(
          502,
          "invalid_response",
          "Macro Terminal received an unexpected related-articles response.",
        );
      }
      return articles.map(parseArticle);
    },
  });
}

function explanationText(payload: unknown): string {
  const explanation = expectRecord(expectRecord(payload).explanation);
  if (typeof explanation.text !== "string") {
    throw new APIError(
      502,
      "invalid_response",
      "Macro Terminal received an unexpected explanation.",
    );
  }
  return explanation.text;
}

export type CalendarEvent = {
  id: string;
  name: string;
  scheduledAt: string;
  releasedAt: string | null;
  previous: number | null;
  consensus: number | null;
  actual: number | null;
  countryCode: string;
  currency: string;
  importance: string;
  revision: number;
  indicator: { id: string; name: string };
  source: { id: string; name: string };
};

export function useExplainArticle() {
  return useMutation({
    mutationFn: async (id: string) => {
      const payload = await apiRequest(
        `/api/v1/articles/${id}/explain`,
        { method: "POST" },
        explanationTimeoutMs,
      );
      return explanationText(payload);
    },
  });
}

export function useExplainEvent() {
  return useMutation({
    mutationFn: async (id: string) => {
      const payload = await apiRequest(
        `/api/v1/calendar-events/${id}/explain`,
        { method: "POST" },
        explanationTimeoutMs,
      );
      return explanationText(payload);
    },
  });
}
