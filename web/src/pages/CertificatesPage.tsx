import {
  ArrowLeftRight,
  Trash,
  TriangleAlert,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useState,
} from "react";

import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { GenerateCard } from "./certificates/GenerateCard";
import { DaysBadge, Field, formatDate, formatDateTime, ICON_BTN_CLS } from "./certificates/display";
import { UploadCard } from "./certificates/UploadCard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api, ApiError, type CertInfo, type CertList } from "@/lib/api";

export default function CertificatesPage({
  refreshSignal,
  showWarnings,
}: {
  refreshSignal: number;
  showWarnings: boolean;
}) {
  const [list, setList] = useState<CertList | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [editingNoteId, setEditingNoteId] = useState<string | null>(null);
  const [noteDraft, setNoteDraft] = useState("");

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

  async function runAction(
    id: string,
    fn: () => Promise<unknown>,
    successMessage?: string,
  ) {
    setActionError(null);
    setBusyId(id);
    try {
      await fn();
      await refresh();
      if (successMessage) toast.success(successMessage);
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : "操作失败，请重试");
    } finally {
      setBusyId(null);
    }
  }

  const handleActivate = (v: CertInfo) =>
    runAction(v.id, () => api.certs.activate(v.id), "证书已启用");

  const startEditNote = (v: CertInfo) => {
    setEditingNoteId(v.id);
    setNoteDraft(v.note);
  };

  const saveNote = (v: CertInfo) => {
    setEditingNoteId(null);
    if (noteDraft.trim() !== v.note) {
      void runAction(
        v.id,
        () => api.certs.rename(v.id, noteDraft.trim()),
        "备注已更新",
      );
    }
  };

  const [pendingDelete, setPendingDelete] = useState<CertInfo | null>(null);

  const handleDelete = (v: CertInfo) => setPendingDelete(v);

  const confirmDelete = () => {
    if (!pendingDelete) return;
    const v = pendingDelete;
    setPendingDelete(null);
    void runAction(v.id, () => api.certs.remove(v.id), "证书已删除");
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
      {showWarnings && active && !active.expired && active.daysLeft < 30 && (
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
      <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
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
                <DaysBadge
                  daysLeft={active.daysLeft}
                  expired={active.expired}
                  showWarnings={showWarnings}
                />
              </Field>
              <Field label="密钥匹配">
                {active.keyMatches ? "是" : "否"}
              </Field>
              <Field label="自签名">{active.selfSigned ? "是" : "否"}</Field>
              <Field label="导入时间">
                <span className="tabular-nums">
                  {formatDateTime(active.importedAt)}
                </span>
              </Field>
            </div>
          </div>
        ) : (
          <p className="mt-3 text-sm text-muted-foreground">暂无生效中的证书</p>
        )}
      </section>

      {/* Versions */}
      <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
        <p className="text-base font-semibold">证书版本</p>
        {actionError && (
          <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
        )}
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[760px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">备注</th>
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
                    {editingNoteId === v.id ? (
                      <div className="flex items-center gap-1">
                        <Input
                          value={noteDraft}
                          autoFocus
                          onChange={(e) => setNoteDraft(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") saveNote(v);
                            if (e.key === "Escape") setEditingNoteId(null);
                          }}
                          placeholder="备注名"
                          className="h-8 w-44"
                        />
                        <Button
                          size="sm"
                          disabled={busyId === v.id}
                          onClick={() => saveNote(v)}
                        >
                          保存
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setEditingNoteId(null)}
                        >
                          取消
                        </Button>
                      </div>
                    ) : (
                      <div
                        title="双击编辑备注"
                        onDoubleClick={() => startEditNote(v)}
                        className="cursor-default select-none"
                      >
                        {v.note || "—"}
                      </div>
                    )}
                  </td>
                  <td className="py-2.5 pr-3">{v.commonName || "—"}</td>
                  <td className="py-2.5 pr-3">{v.issuer || "—"}</td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {formatDate(v.notAfter)}
                  </td>
                  <td className="py-2.5 pr-3">
                    <DaysBadge
                      daysLeft={v.daysLeft}
                      expired={v.expired}
                      showWarnings={showWarnings}
                    />
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
                        <button
                          type="button"
                          title="启用"
                          aria-label="启用"
                          disabled={busyId === v.id}
                          onClick={() => void handleActivate(v)}
                          className={`${ICON_BTN_CLS} disabled:opacity-50`}
                        >
                          <ArrowLeftRight className="h-4 w-4" />
                        </button>
                        <button
                          type="button"
                          title="删除"
                          aria-label="删除"
                          disabled={busyId === v.id}
                          onClick={() => handleDelete(v)}
                          className={`${ICON_BTN_CLS} disabled:opacity-50`}
                        >
                          <Trash className="h-4 w-4" />
                        </button>
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

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title="删除证书"
        description={
          pendingDelete
            ? `确定要删除证书「${pendingDelete.note || pendingDelete.commonName || pendingDelete.id}」吗？此操作不可恢复。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={confirmDelete}
      />
    </div>
  );
}

