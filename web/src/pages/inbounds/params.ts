import type {
  AnyTLSParams,
  Hy2Params,
  InboundParams,
  InboundProtocol,
  RealityParams,
  TrojanParams,
  TuicParams,
} from "@/lib/api";

export interface InboundFormState {
  protocol: InboundProtocol;
  sni: string;
  flow: string;
  publicKey: string;
  privateKey: string;
  shortId: string;
  upMbps: number;
  downMbps: number;
  password: string;
  obfs: string;
  obfsPassword: string;
  tuicUuid: string;
  tuicPassword: string;
  tuicCongestion: string;
  tuicSni: string;
  alPassword: string;
  alSni: string;
  tjPassword: string;
  tjSni: string;
}

export function buildParams(state: InboundFormState): InboundParams {
  const {
    protocol,
    sni,
    flow,
    publicKey,
    privateKey,
    shortId,
    upMbps,
    downMbps,
    password,
    obfs,
    obfsPassword,
    tuicUuid,
    tuicPassword,
    tuicCongestion,
    tuicSni,
    alPassword,
    alSni,
    tjPassword,
    tjSni,
  } = state;
    switch (protocol) {
      case "vless-reality":
        return {
          sni: sni.trim(),
          publicKey: publicKey.trim(),
          privateKey: privateKey.trim(),
          shortId: shortId.trim(),
          flow: flow.trim() || "xtls-rprx-vision",
        } as RealityParams;
      case "hysteria2":
        return {
          upMbps,
          downMbps,
          password: password.trim(),
          obfs,
          // Unknown obfs values keep their password: only an explicit
          // "none" clears it, so an unrecognized value is never
          // silently destroyed by saving the form.
          obfsPassword: obfs === "" ? "" : obfsPassword.trim(),
        } as Hy2Params;
      case "tuic":
        return {
          uuid: tuicUuid.trim(),
          password: tuicPassword.trim(),
          congestionControl: tuicCongestion,
          sni: tuicSni.trim(),
        } as TuicParams;
      case "anytls":
        return {
          password: alPassword.trim(),
          sni: alSni.trim(),
        } as AnyTLSParams;
      case "trojan":
        return {
          password: tjPassword.trim(),
          sni: tjSni.trim(),
        } as TrojanParams;
    }
}
