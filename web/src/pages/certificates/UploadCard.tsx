import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, ApiError } from "@/lib/api";
import { PemField } from "./PemField";
import i18n from "@/lib/i18n";

export function UploadCard({ onUploaded }: { onUploaded: () => Promise<void> }) {
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
      setFormSuccess(activate ? i18n.t('certificates.k044') : i18n.t('certificates.k045'));
      await onUploaded();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : i18n.t('certificates.k046'));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <p className="text-base font-semibold">{i18n.t('certificates.k047')}</p>
      <form onSubmit={handleSubmit} className="mt-4 space-y-4">
        <div className="grid gap-4 md:grid-cols-2">
          <PemField
            id="cert-pem"
            label={i18n.t('certificates.k048')}
            placeholder="-----BEGIN CERTIFICATE-----"
            value={certPem}
            onChange={setCertPem}
            onError={setFormError}
          />
          <PemField
            id="key-pem"
            label={i18n.t('certificates.k049')}
            placeholder="-----BEGIN PRIVATE KEY-----"
            value={keyPem}
            onChange={setKeyPem}
            onError={setFormError}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="cert-note">{i18n.t('certificates.k050')}</Label>
          <Input
            id="cert-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={i18n.t('certificates.k051')}
          />
        </div>
        <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={activate}
            onChange={(e) => setActivate(e.target.checked)}
            className="h-4 w-4 rounded border accent-primary"
          />
          {i18n.t('certificates.k064')}
        </label>
        {formError && <p className="text-[13px] text-destructive">{formError}</p>}
        {formSuccess && (
          <p className="text-[13px] text-emerald-600 dark:text-emerald-400">
            {formSuccess}
          </p>
        )}
        <Button type="submit" disabled={submitting}>
          {submitting ? i18n.t('certificates.k052') : i18n.t('certificates.k053')}
        </Button>
      </form>
    </section>
  );
}
