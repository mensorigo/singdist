import { request } from "./client";

export interface AuthStatus {
  authenticated: boolean;
  username?: string;
}


export const authApi = {
  status: () => request<AuthStatus>("/api/auth/status"),
  login: (username: string, password: string) =>
    request<{ username: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<{ ok: boolean }>("/api/auth/password", {
      method: "PATCH",
      body: JSON.stringify({ currentPassword, newPassword }),
    }),
  changeUsername: (username: string, currentPassword: string) =>
    request<{ username: string }>("/api/auth/username", {
      method: "PATCH",
      body: JSON.stringify({ username, currentPassword }),
    }),
  changeAccount: (body: {
    username?: string;
    currentPassword: string;
    newPassword?: string;
  }) =>
    request<{ username: string }>("/api/auth/account", {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  revokeOtherSessions: () =>
    request<{ revoked: number }>("/api/auth/sessions/revoke-others", {
      method: "POST",
    }),
};
