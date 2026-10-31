import { request } from "./client";

export interface TemplateGroup {
  name: string;
  type: string;
  inboundIds: number[];
  enabled: boolean;
  url?: string;
  interval?: number;
  tolerance?: number;
}

export interface TemplateView {
  id: number;
  name: string;
  inboundIds: number[];
  groups: TemplateGroup[];
  subscriptionCount: number;
  createdAt: number;
  updatedAt: number;
}

export interface TemplateRequest {
  name: string;
  inboundIds: number[];
  groups?: TemplateGroup[];
}


export const templatesApi = {
list: () => request<TemplateView[]>("/api/templates"),
    create: (body: TemplateRequest) =>
      request<TemplateView>("/api/templates", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    update: (id: number, body: TemplateRequest) =>
      request<TemplateView>(`/api/templates/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    remove: (id: number) =>
      request<{ ok: boolean }>(`/api/templates/${id}`, { method: "DELETE" }),
};
