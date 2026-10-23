import { ClipboardPaste, TriangleAlert, Upload } from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, ApiError, type CertInfo, type CertList } from "@/lib/api";
import { cn } from "@/lib/utils";

function formatDateTime(unix: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleString("zh-CN", { hour12: false });
}

function formatDate(unix: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleDateString("zh-CN");
}

function sourceLabel(source: string): string {
  if (source === "bootstrap") return "启动导入";
  if (source === "selfsigned") return "自签名生成";
  return "上传";
}

function DaysBadge({
  daysLeft,
  expired,
}: {
  daysLeft: number;
  expired: boolean;
}) {
  if (expired) {
    return (
      <span className="inline-flex rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
        已过期
      </span>
    );
  }
  const cls =
    daysLeft < 15
      ? "bg-destructive/10 text-destructive"
      : daysLeft <= 60
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2 py-0.5 text-xs font-medium tabular-nums",
        cls,
      )}
    >
      {daysLeft} 天
    </span>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="mt-1 text-sm">{children}</div>
    </div>
  );
}

const TEXTAREA_CLS =
  "w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50";

export default function CertificatesPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [list, setList] = useState<CertList | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const data = await api.certs.list();
      setList(data);
      setLoadError(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        window.location.reload();
        return;
      }
      setLoadError(true);
    }
  }, []);

  // Mount + top-bar refresh signal: refetch in place; the upload form
  // below is a separate component and keeps whatever was typed into it.
  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

  async function runAction(id: string, fn: () => Promise<unknown>) {
    setActionError(null);
    setBusyId(id);
    try {
      await fn();
      await refresh();
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : "操作失败，请重试");
    } finally {
      setBusyId(null);
    }
  }

  const handleActivate = (v: CertInfo) =>
    runAction(v.id, () => api.certs.activate(v.id));

  const handleDelete = (v: CertInfo) => {
    const name = v.note || v.commonName || v.id;
    if (!window.confirm(`确定要删除证书「${name}」吗？此操作不可恢复。`)) {
      return;
    }
    void runAction(v.id, () => api.certs.remove(v.id));
  };

  if (!list) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? "暂时无法获取证书数据，请稍后刷新重试" : "数据加载中…"}
      </p>
    );
  }

  const active = list.active;

  return (
    <div className="space-y-4">
      {active && active.expired && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-md border border-destructive/50 bg-destructive/10 px-3.5 py-2.5 text-[13px] text-destructive"
        >
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <p className="font-medium">当前证书已过期，请立即更换。</p>
        </div>
      )}
      {active && !active.expired && active.daysLeft < 30 && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-md border border-amber-500/50 bg-amber-500/10 px-3.5 py-2.5 text-[13px] text-amber-700 dark:text-amber-400"
        >
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <p className="font-medium">
            证书将在 {active.daysLeft} 天后过期，请及时更新。
          </p>
        </div>
      )}

      {/* Current certificate */}
      <section className="rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
        <p className="text-base font-semibold">当前证书</p>
        {active ? (
          <div className="mt-4 space-y-5">
            <div>
              <p className="text-xs text-muted-foreground">主域名</p>
              <p className="mt-1 break-all text-2xl font-semibold tracking-tight">
                {active.commonName || "—"}
              </p>
              {active.dnsNames.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {active.dnsNames.map((name) => (
                    <span
                      key={name}
                      className="rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                    >
                      {name}
                    </span>
                  ))}
                </div>
              )}
            </div>

            <div className="grid grid-cols-1 gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-3">
              <Field label="颁发者">{active.issuer || "—"}</Field>
              <Field label="序列号">
                <span className="break-all font-mono text-xs">
                  {active.serial}
                </span>
              </Field>
              <Field label="密钥类型">{active.keyType}</Field>
              <Field label="生效时间">
                <span className="tabular-nums">
                  {formatDateTime(active.notBefore)}
                </span>
              </Field>
              <Field label="到期时间">
                <span className="tabular-nums">
                  {formatDateTime(active.notAfter)}
                </span>
              </Field>
              <Field label="剩余天数">
                <DaysBadge daysLeft={active.daysLeft} expired={active.expired} />
              </Field>
              <Field label="密钥匹配">
                {active.keyMatches ? "是" : "否"}
              </Field>
              <Field label="自签名">{active.selfSigned ? "是" : "否"}</Field>
              <Field label="来源">
                {sourceLabel(active.source)}
                {active.note ? ` · ${active.note}` : ""}
              </Field>
              <Field label="导入时间">
                <span className="tabular-nums">
                  {formatDateTime(active.importedAt)}
                </span>
              </Field>
              <div className="sm:col-span-2 lg:col-span-3">
                <Field label="SHA-256 指纹">
                  <span className="break-all font-mono text-xs">
                    {active.fingerprint}
                  </span>
                </Field>
              </div>
            </div>
          </div>
        ) : (
          <p className="mt-3 text-sm text-muted-foreground">暂无生效中的证书</p>
        )}
      </section>

      {/* Versions */}
      <section className="rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
        <p className="text-base font-semibold">证书版本</p>
        {actionError && (
          <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
        )}
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[760px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">备注 / 来源</th>
                <th className="py-2 pr-3 font-medium">主域名</th>
                <th className="py-2 pr-3 font-medium">颁发者</th>
                <th className="py-2 pr-3 font-medium">到期</th>
                <th className="py-2 pr-3 font-medium">剩余</th>
                <th className="py-2 pr-3 font-medium">导入时间</th>
                <th className="py-2 pr-3 font-medium">状态</th>
                <th className="py-2 font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {list.versions.map((v) => (
                <tr key={v.id} className="border-b last:border-0">
                  <td className="py-2.5 pr-3">
                    <div>{v.note || "—"}</div>
                    <div className="text-xs text-muted-foreground">
                      {sourceLabel(v.source)}
                    </div>
                  </td>
                  <td className="py-2.5 pr-3">{v.commonName || "—"}</td>
                  <td className="py-2.5 pr-3">{v.issuer || "—"}</td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {formatDate(v.notAfter)}
                  </td>
                  <td className="py-2.5 pr-3">
                    <DaysBadge daysLeft={v.daysLeft} expired={v.expired} />
                  </td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {formatDateTime(v.importedAt)}
                  </td>
                  <td className="py-2.5 pr-3">
                    {v.active ? (
                      <span className="inline-flex rounded-full bg-primary px-2 py-0.5 text-xs font-medium text-primary-foreground">
                        生效中
                      </span>
                    ) : (
                      <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                        备用
                      </span>
                    )}
                  </td>
                  <td className="py-2.5">
                    {!v.active && (
                      <div className="flex items-center gap-1">
                        <Button
                          size="sm"
                          disabled={busyId === v.id}
                          onClick={() => void handleActivate(v)}
                        >
                          启用
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busyId === v.id}
                          onClick={() => handleDelete(v)}
                          className="text-destructive hover:text-destructive"
                        >
                          删除
                        </Button>
                      </div>
                    )}
                  </td>
                </tr>
              ))}
              {list.versions.length === 0 && (
                <tr>
                  <td
                    colSpan={8}
                    className="py-6 text-center text-sm text-muted-foreground"
                  >
                    暂无证书版本
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <UploadCard onUploaded={refresh} />

      <GenerateCard onGenerated={refresh} />
    </div>
  );
}

// One PEM input with paste-from-clipboard and file-upload shortcuts.
function PemField({
  id,
  label,
  placeholder,
  value,
  onChange,
  onError,
}: {
  id: string;
  label: string;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
  onError: (msg: string) => void;
}) {
  const fileRef = useRef<HTMLInputElement>(null);

  async function handlePaste() {
    try {
      const text = await navigator.clipboard.readText();
      if (text) onChange(text);
    } catch {
      onError("读取剪贴板失败：浏览器未授权，请用“上传文件”或手动粘贴（Ctrl+V）");
    }
  }

  async function handleFile(file: File | undefined) {
    if (!file) return;
    try {
      onChange(await file.text());
    } catch {
      onError("读取文件失败，请重试");
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label htmlFor={id}>{label}</Label>
        <div className="flex gap-1.5">
          <Button
            type="button"
            variant="outline"
            onClick={handlePaste}
            className="h-7 px-2 text-xs"
          >
            <ClipboardPaste />
            粘贴
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => fileRef.current?.click()}
            className="h-7 px-2 text-xs"
          >
            <Upload />
            上传文件
          </Button>
        </div>
      </div>
      <textarea
        id={id}
        rows={6}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className={TEXTAREA_CLS}
        spellCheck={false}
      />
      <input
        ref={fileRef}
        type="file"
        accept=".pem,.crt,.cer,.key,text/plain"
        className="hidden"
        onChange={(e) => {
          void handleFile(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
    </div>
  );
}

// Generate a self-signed certificate in place: the names are the
// addresses the panel will be visited by (domain or IP) — clients check
// them against the address they connected to.
function GenerateCard({ onGenerated }: { onGenerated: () => Promise<void> }) {
  const [namesText, setNamesText] = useState("");
  const [days, setDays] = useState("365");
  const [note, setNote] = useState("");
  const [activate, setActivate] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSuccess, setFormSuccess] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);
    setFormSuccess(null);
    const names = namesText
      .split(/[\s,，]+/)
      .map((s) => s.trim())
      .filter(Boolean);
    if (names.length === 0) {
      setFormError("请至少填写一个域名或 IP");
      return;
    }
    const daysNum = Number.parseInt(days, 10);
    if (!Number.isFinite(daysNum) || daysNum < 1 || daysNum > 3650) {
      setFormError("有效期天数需在 1 到 3650 之间");
      return;
    }
    setSubmitting(true);
    try {
      await api.certs.selfSigned(names, daysNum, note.trim(), activate);
      setNamesText("");
      setNote("");
      setActivate(false);
      setFormSuccess(activate ? "自签名证书已生成并启用" : "自签名证书已生成并保存");
      await onGenerated();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "生成失败，请重试");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <p className="text-base font-semibold">生成自签名证书</p>
      <p className="mt-1 text-[13px] text-muted-foreground">
        名字填你将来访问面板时用的域名或 IP（客户端会核对它与地址是否一致），
        多个用逗号或空格分隔。无需向任何机构申请，但浏览器不会自动信任自签名证书。
      </p>
      <form onSubmit={handleSubmit} className="mt-4 space-y-4">
        <div className="grid gap-4 md:grid-cols-[1fr_160px]">
          <div className="space-y-2">
            <Label htmlFor="ss-names">域名或 IP</Label>
            <Input
              id="ss-names"
              value={namesText}
              onChange={(e) => setNamesText(e.target.value)}
              placeholder="例如：panel.example.com 或 192.168.1.10"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="ss-days">有效期（天）</Label>
            <Input
              id="ss-days"
              type="number"
              min={1}
              max={3650}
              value={days}
              onChange={(e) => setDays(e.target.value)}
            />
          </div>
        </div>
        <div className="space-y-2">
          <Label htmlFor="ss-note">备注（可选）</Label>
          <Input
            id="ss-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="例如：内网测试用"
          />
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={activate}
            onChange={(e) => setActivate(e.target.checked)}
            className="h-4 w-4 rounded border accent-primary"
          />
          生成并立即启用
        </label>
        {formError && <p className="text-[13px] text-destructive">{formError}</p>}
        {formSuccess && (
          <p className="text-[13px] text-emerald-600 dark:text-emerald-400">
            {formSuccess}
          </p>
        )}
        <Button type="submit" disabled={submitting}>
          {submitting ? "生成中…" : "生成证书"}
        </Button>
      </form>
    </section>
  );
}

function UploadCard({ onUploaded }: { onUploaded: () => Promise<void> }) {
  const [certPem, setCertPem] = useState("");
  const [keyPem, setKeyPem] = useState("");
  const [note, setNote] = useState("");
  const [activate, setActivate] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSuccess, setFormSuccess] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setFormError(null);
    setFormSuccess(null);
    setSubmitting(true);
    try {
      await api.certs.upload(certPem.trim(), keyPem.trim(), note.trim(), activate);
      setCertPem("");
      setKeyPem("");
      setNote("");
      setActivate(false);
      setFormSuccess(activate ? "证书已保存并启用" : "证书已保存");
      await onUploaded();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "上传失败，请重试");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <p className="text-base font-semibold">上传新证书</p>
      <form onSubmit={handleSubmit} className="mt-4 space-y-4">
        <div className="grid gap-4 md:grid-cols-2">
          <PemField
            id="cert-pem"
            label="证书 PEM"
            placeholder="-----BEGIN CERTIFICATE-----"
            value={certPem}
            onChange={setCertPem}
            onError={setFormError}
          />
          <PemField
            id="key-pem"
            label="私钥 PEM"
            placeholder="-----BEGIN PRIVATE KEY-----"
            value={keyPem}
            onChange={setKeyPem}
            onError={setFormError}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="cert-note">备注（可选）</Label>
          <Input
            id="cert-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="例如：2026 年续期证书"
          />
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={activate}
            onChange={(e) => setActivate(e.target.checked)}
            className="h-4 w-4 rounded border accent-primary"
          />
          保存并立即启用
        </label>
        {formError && <p className="text-[13px] text-destructive">{formError}</p>}
        {formSuccess && (
          <p className="text-[13px] text-emerald-600 dark:text-emerald-400">
            {formSuccess}
          </p>
        )}
        <Button type="submit" disabled={submitting}>
          {submitting ? "上传中…" : "上传证书"}
        </Button>
      </form>
    </section>
  );
}
