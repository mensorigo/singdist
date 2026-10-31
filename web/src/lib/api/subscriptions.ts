import { request } from "./client";

export interface SubscriptionPreview {
  format: string;
  contentType: string;
  content: string;
}

export type SubscriptionStatus =
  | "active"
  | "disabled"
  | "expired"
  | "over_quota";

export interface SubscriptionView {
  id: number;
  name: string;
  uuid: string;
  token: string;
  templateId: number;
  templateName: string;
  limitBytes: number;
  usedUp: number;
  usedDown: number;
  usedBytes: number;
  expireAt: number;
  maxIps: number;
  enabled: boolean;
  status: SubscriptionStatus;
  createdAt: number;
  updatedAt: number;
}

export interface SubscriptionRequest {
  name: string;
  templateId: number;
  limitBytes: number;
  expireAt: number;
  maxIps: number;
}


export const subscriptionsApi = {
list: () => request<SubscriptionView[]>("/api/subscriptions"),
    create: (body: SubscriptionRequest) =>
      request<SubscriptionView>("/api/subscriptions", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    update: (id: number, body: SubscriptionRequest) =>
      request<SubscriptionView>(`/api/subscriptions/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    remove: (id: number) =>
      request<{ ok: boolean }>(`/api/subscriptions/${id}`, {
        method: "DELETE",
      }),
    setEnabled: (id: number, enabled: boolean) =>
      request<SubscriptionView>(`/api/subscriptions/${id}/enabled`, {
        method: "PATCH",
        body: JSON.stringify({ enabled }),
      }),
    resetToken: (id: number) =>
      request<SubscriptionView>(`/api/subscriptions/${id}/reset-token`, {
        method: "POST",
      }),
    resetUuid: (id: number) =>
      request<SubscriptionView>(`/api/subscriptions/${id}/reset-uuid`, {
        method: "POST",
      }),
    preview: (id: number) =>
      request<SubscriptionPreview>(`/api/subscriptions/${id}/preview`),
    onlineIps: () =>
      request<{
        counts: Record<string, number>;
        total: number;
      }>("/api/subscriptions/online-ips"),
};
