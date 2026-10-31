import { request } from "./client";

export interface SettingsView {
  webrtcMode: "proxy" | "reject" | "off";
  dnsFallback: "foreign" | "domestic";
  dnsRespectRules: boolean;
  linkFormat: "clash" | "singbox";
  skipCertVerify: boolean;
  publicHost: string;
  showUserinfo: boolean;
  updateIntervalHours: number;
  textSelectable: boolean;
  timezone: string;
  showWarnings: boolean;
  dashIntervalSecs: number;
  autoSyncEnabled: boolean;
  autoSyncIntervalSecs: number;
}

export interface SettingsUpdate {
  webrtcMode?: SettingsView["webrtcMode"];
  dnsFallback?: SettingsView["dnsFallback"];
  dnsRespectRules?: boolean;
  linkFormat?: SettingsView["linkFormat"];
  skipCertVerify?: boolean;
  publicHost?: string;
  showUserinfo?: boolean;
  updateIntervalHours?: number;
  textSelectable?: boolean;
  timezone?: string;
  showWarnings?: boolean;
  dashIntervalSecs?: number;
  autoSyncEnabled?: boolean;
  autoSyncIntervalSecs?: number;
}


export const settingsApi = {
get: () => request<SettingsView>("/api/settings"),
    update: (body: SettingsUpdate) =>
      request<SettingsView>("/api/settings", {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
};
