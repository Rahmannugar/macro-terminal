import { useMemo } from "react";
import { useReferenceRows } from "../lib/admin";

export type EntityRow = {
  id: string;
  code: string;
  name: string;
  type: string;
  createdAt: string;
  updatedAt: string;
};

export type IndicatorRow = {
  id: string;
  name: string;
  type: string;
  entityId: string;
  createdAt: string;
  updatedAt: string;
};

export type SourceRow = {
  id: string;
  name: string;
  type: string;
  createdAt: string;
  updatedAt: string;
};

export function useEntityRows(): EntityRow[] {
  return useReferenceRows<EntityRow>(
    ["admin", "entities", "reference"],
    "/api/v1/admin/entities",
    "entities",
  );
}

export function useIndicatorRows(): IndicatorRow[] {
  return useReferenceRows<IndicatorRow>(
    ["admin", "indicators", "reference"],
    "/api/v1/admin/indicators",
    "indicators",
  );
}

export function useSourceRows(): SourceRow[] {
  return useReferenceRows<SourceRow>(
    ["admin", "sources", "reference"],
    "/api/v1/admin/sources",
    "sources",
  );
}

export function useEntityCodes(): Map<string, string> {
  const entities = useEntityRows();
  return useMemo(() => new Map(entities.map((entity) => [entity.id, entity.code])), [entities]);
}

export function useIndicatorNames(): Map<string, string> {
  const indicators = useIndicatorRows();
  return useMemo(
    () => new Map(indicators.map((indicator) => [indicator.id, indicator.name])),
    [indicators],
  );
}

export function useSourceNames(): Map<string, string> {
  const sources = useSourceRows();
  return useMemo(() => new Map(sources.map((source) => [source.id, source.name])), [sources]);
}

export function labelFor(labels: Map<string, string>, id: string): string {
  return labels.get(id) ?? `${id.slice(0, 8)}…`;
}
