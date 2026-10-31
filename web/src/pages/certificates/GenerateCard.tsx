import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, ApiError } from "@/lib/api";
import i18n from "@/lib/i18n";

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
        i18n.t('certificates.k034', { name: info.commonName, days: info.daysLeft }),
      );
      toast.success(i18n.t('certificates.k035'));
      await onGenerated();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : i18n.t('certificates.k036'));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <p className="text-base font-semibold">{i18n.t('certificates.k037')}</p>
      <p className="mt-1 text-[13px] text-muted-foreground">
        {i18n.t('certificates.k060')}
        {i18n.t('certificates.k061')}
      </p>
      <div className="mt-4 max-w-md space-y-2">
        <Label htmlFor="ss-note">{i18n.t('certificates.k038')}</Label>
        <Input
          id="ss-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={i18n.t('certificates.k039')}
        />
      </div>
      {formError && <p className="mt-3 text-[13px] text-destructive">{formError}</p>}
      {formSuccess && (
        <p className="mt-3 text-[13px] text-emerald-600 dark:text-emerald-400">
          {formSuccess}
        </p>
      )}
      <Button className="mt-4" onClick={handleGenerate} disabled={submitting}>
        {submitting ? i18n.t('certificates.k040') : i18n.t('certificates.k041')}
      </Button>
    </section>
  );
}

