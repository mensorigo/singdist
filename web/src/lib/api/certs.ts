import { request } from "./client";

export interface CertInfo {
  id: string;
  note: string;
  source: string;
  importedAt: number;
  active: boolean;
  commonName: string;
  dnsNames: string[];
  issuer: string;
  serial: string;
  notBefore: number;
  notAfter: number;
  daysLeft: number;
  fingerprint: string;
  keyType: string;
  keyMatches: boolean;
  selfSigned: boolean;
  expired: boolean;
}

export interface CertList {
  active: CertInfo | null;
  versions: CertInfo[];
}


export const certsApi = {
list: () => request<CertList>("/api/certs"),
    upload: (certPem: string, keyPem: string, note: string, activate: boolean) =>
      request<CertInfo>("/api/certs", {
        method: "POST",
        body: JSON.stringify({ certPem, keyPem, note, activate }),
      }),
    selfSigned: (
      names: string[],
      days: number,
      note: string,
      activate: boolean,
    ) =>
      request<CertInfo>("/api/certs/selfsigned", {
        method: "POST",
        body: JSON.stringify({ names, days, note, activate }),
      }),
    rename: (id: string, note: string) =>
      request<CertInfo>(`/api/certs/${encodeURIComponent(id)}/note`, {
        method: "PATCH",
        body: JSON.stringify({ note }),
      }),
    activate: (id: string) =>
      request<CertInfo>(`/api/certs/${encodeURIComponent(id)}/activate`, {
        method: "POST",
      }),
    remove: (id: string) =>
      request<{ ok: boolean }>(`/api/certs/${encodeURIComponent(id)}`, {
        method: "DELETE",
      }),
};
