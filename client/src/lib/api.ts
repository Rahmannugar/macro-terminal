const requestTimeout = 15_000;
const developmentBaseURL = "http://localhost:8081";

export class APIError extends Error {
  readonly status: number;
  readonly code?: string;

  constructor(status: number, code?: string, message?: string) {
    super(message ?? "The request could not be completed.");
    this.status = status;
    this.code = code;
  }
}

export function apiURL(path: string): string {
  const configured = import.meta.env.VITE_API_URL;
  const base =
    configured || (import.meta.env.DEV ? developmentBaseURL : window.location.origin);
  return new URL(path, base).toString();
}

export async function apiRequest(
  path: string,
  init: RequestInit = {},
  timeoutMs = requestTimeout,
): Promise<unknown> {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetch(apiURL(path), {
      ...init,
      credentials: "include",
      signal: controller.signal,
      headers: {
        ...(init.body ? { "Content-Type": "application/json" } : {}),
        ...init.headers,
      },
    });
    if (!response.ok) {
      const detail = await readErrorDetail(response);
      throw new APIError(response.status, detail?.code, detail?.message);
    }
    if (response.status === 204) return null;
    const payload = await response.text();
    return payload ? (JSON.parse(payload) as unknown) : null;
  } catch (error) {
    if (error instanceof APIError) throw error;
    if (error instanceof DOMException && error.name === "AbortError") {
      throw new APIError(0, "request_timeout", "The request took too long. Try again.");
    }
    throw new APIError(
      0,
      "network_error",
      "Macro Terminal could not reach the server. Try again.",
    );
  } finally {
    window.clearTimeout(timeout);
  }
}

type ErrorDetail = { code?: string; message?: string };

async function readErrorDetail(response: Response): Promise<ErrorDetail | null> {
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    return null;
  }
  if (typeof payload !== "object" || payload === null) return null;
  const envelope = (payload as { error?: unknown }).error;
  if (typeof envelope !== "object" || envelope === null) return null;
  const detail = envelope as Record<string, unknown>;
  return {
    code: typeof detail.code === "string" ? detail.code : undefined,
    message: typeof detail.message === "string" ? detail.message : undefined,
  };
}
