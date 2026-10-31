import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, ApiError } from "@/lib/api";

// One-click self-signed certificate: the name is the address the admin
// is using to view this page right now (domain or IP), validity 365
// days, stored inactive — switching to it stays an explicit action in
// the versions table above.
export function GenerateCard({ onGenerated }: { onGenerated: () => Promise<void> }) {
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSuccess, setFormSuccess] = useState<string | null>(null);

  async function handleGenerate() {
    setFormError(null);
    setFormSuccess(null);
    setSubmitting(true);
    try {
      const host = window.location.hostname;
      const info = await api.certs.selfSigned([host], 365, note.trim(), false);
      setNote("");
      setFormSuccess(
        `已生成：${info.commonName}（有效期 ${info.daysLeft} 天），已存入上方版本列表，需要时点“启用”即可切换`,
      );
      toast.success("证书生成成功");
      await onGenerated();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "生成失败，请重试");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <p className="text-base font-semibold">生成自签名证书</p>
      <p className="mt-1 text-[13px] text-muted-foreground">
        按你当前访问面板的地址（域名或 IP）一键生成一张自签名证书，有效期
        365 天。生成后会进入上方版本列表，不会自动切换当前证书。
      </p>
      <div className="mt-4 max-w-md space-y-2">
        <Label htmlFor="ss-note">备注名（可选）</Label>
        <Input
          id="ss-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="不填则自动编号，如 CERT-8F3K"
        />
      </div>
      {formError && <p className="mt-3 text-[13px] text-destructive">{formError}</p>}
      {formSuccess && (
        <p className="mt-3 text-[13px] text-emerald-600 dark:text-emerald-400">
          {formSuccess}
        </p>
      )}
      <Button className="mt-4" onClick={handleGenerate} disabled={submitting}>
        {submitting ? "生成中…" : "一键生成自签名证书"}
      </Button>
    </section>
  );
}

