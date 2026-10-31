import { request } from "./client";

export interface SyncLogEntry {
  id: number;
  at: number;
  action: string;
  result: string;
  hash: string;
  detail: string;
}

export interface SyncStatus {
  targetPath: string;
  restartEnabled: boolean;
  desiredHash: string;
  targetHash: string;
  inSync: boolean;
  last: SyncLogEntry | null;
}


export const syncApi = {
status: () => request<SyncStatus>("/api/sync/status"),
    run: () => request<SyncLogEntry>("/api/sync", { method: "POST" }),
};
