export class ApiError extends Error {
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// Config-change notifications: any successful mutation on a
// config-affecting endpoint tells subscribers (the top-bar sync
// control) to reload the sync status immediately, instead of waiting
// for a manual refresh. Reads and auth calls never emit.
type ConfigChangeListener = () => void;

const configChangeListeners = new Set<ConfigChangeListener>();

export function onConfigChanged(fn: ConfigChangeListener): () => void {
  configChangeListeners.add(fn);
  return () => {
    configChangeListeners.delete(fn);
  };
}

const CONFIG_PATHS = [
  "/api/inbounds",
  "/api/subscriptions",
  "/api/templates",
  "/api/certs",
  "/api/settings",
  "/api/sync",
];

function emitConfigChanged(path: string, method: string | undefined) {
  if (!method || method === "GET") return;
  if (!CONFIG_PATHS.some((p) => path === p || path.startsWith(p + "/"))) {
    return;
  }
  for (const fn of configChangeListeners) {
    try {
      fn();
    } catch {
      /* a listener must never break the request */
    }
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const data = (await resp.json().catch(() => ({}))) as Record<string, unknown>;
  if (!resp.ok) {
    throw new ApiError(
      resp.status,
      typeof data.code === "string" ? data.code : "request_failed",
      typeof data.message === "string" ? data.message : resp.statusText,
    );
  }
  emitConfigChanged(path, init?.method);
  return data as T;
}
