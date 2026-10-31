import { useCallback, useEffect, useState } from "react";
import { Globe, Share2, Shield, Type } from "lucide-react";
import { toast } from "sonner";

import { EnabledSwitch } from "@/components/enabled-switch";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  api,
  ApiError,
  type SettingsUpdate,
  type SettingsView,
} from "@/lib/api";
import { Segmented } from "./settings/Segmented";
import { SettingCard } from "./settings/SettingCard";
import { Row } from "./settings/Row";
import i18n, { setLanguage, type SupportedLanguage } from "@/lib/i18n";

// Curated IANA zones for the panel timezone dropdown. The stored value
// is always offered as well, so a zone set elsewhere never renders blank.
function timezoneOptions(): { value: string; label: string }[] {
  return [
  { value: "Asia/Shanghai", label: i18n.t('settings.k001') },
  { value: "Asia/Taipei", label: i18n.t('settings.k002') },
  { value: "Asia/Hong_Kong", label: i18n.t('settings.k003') },
  { value: "Asia/Tokyo", label: i18n.t('settings.k004') },
  { value: "Asia/Seoul", label: i18n.t('settings.k005') },
  { value: "Asia/Singapore", label: i18n.t('settings.k006') },
  { value: "Asia/Bangkok", label: i18n.t('settings.k007') },
  { value: "Asia/Dubai", label: i18n.t('settings.k008') },
  { value: "Asia/Kolkata", label: i18n.t('settings.k009') },
  { value: "Europe/London", label: i18n.t('settings.k010') },
  { value: "Europe/Paris", label: i18n.t('settings.k011') },
  { value: "Europe/Berlin", label: i18n.t('settings.k012') },
  { value: "Europe/Moscow", label: i18n.t('settings.k013') },
  { value: "Africa/Cairo", label: i18n.t('settings.k014') },
  { value: "America/New_York", label: i18n.t('settings.k015') },
  { value: "America/Chicago", label: i18n.t('settings.k016') },
  { value: "America/Denver", label: i18n.t('settings.k017') },
  { value: "America/Los_Angeles", label: i18n.t('settings.k018') },
  { value: "America/Sao_Paulo", label: i18n.t('settings.k019') },
  { value: "Australia/Sydney", label: i18n.t('settings.k020') },
  { value: "Pacific/Auckland", label: i18n.t('settings.k021') },
  { value: "UTC", label: i18n.t('settings.k022') },
];
}

export default function SettingsPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [settings, setSettings] = useState<SettingsView | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [saving, setSaving] = useState(false);

  // Drafts for the text/number fields; committed on blur, resynced
  // whenever the saved settings change (including revert-on-error).
  const [publicHostDraft, setPublicHostDraft] = useState("");
  const [intervalDraft, setIntervalDraft] = useState("24");
  const [dashDraft, setDashDraft] = useState("3");

  const refresh = useCallback(async () => {
    try {
      const v = await api.settings.get();
      setSettings(v);
      setLoadError(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        window.location.reload();
        return;
      }
      setLoadError(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

  useEffect(() => {
    if (settings) {
      setPublicHostDraft(settings.publicHost);
      setIntervalDraft(String(settings.updateIntervalHours));
      setDashDraft(String(settings.dashIntervalSecs));
    }
  }, [settings]);

  // Instant save: apply optimistically, persist, revert on failure.
  const apply = (patch: SettingsUpdate) => {
    if (!settings || saving) return;
    const prev = settings;
    setSettings({ ...settings, ...patch });
    setSaving(true);
    void (async () => {
      try {
        const v = await api.settings.update(patch);
        setSettings(v);
        toast.success(i18n.t('settings.k023'));
      } catch (err) {
        setSettings(prev);
        toast.error(
          err instanceof ApiError ? err.message : i18n.t('settings.k024'),
        );
      } finally {
        setSaving(false);
      }
    })();
  };

  if (!settings) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? i18n.t('settings.k025') : i18n.t('settings.k026')}
      </p>
    );
  }

  const commitInterval = () => {
    const n = Number(intervalDraft);
    if (!Number.isInteger(n) || n < 0 || n > 720) {
      toast.error(i18n.t('settings.k027'));
      setIntervalDraft(String(settings.updateIntervalHours));
      return;
    }
    if (n !== settings.updateIntervalHours) {
      apply({ updateIntervalHours: n });
    }
  };

  const commitDashInterval = () => {
    const n = Number(dashDraft);
    if (!Number.isInteger(n) || n < 1 || n > 60) {
      toast.error(i18n.t('settings.k028'));
      setDashDraft(String(settings.dashIntervalSecs));
      return;
    }
    if (n !== settings.dashIntervalSecs) {
      apply({ dashIntervalSecs: n });
    }
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        <SettingCard
          icon={<Shield className="h-[18px] w-[18px]" />}
          title="WebRTC / STUN"
          desc={i18n.t('settings.k029')}
        >
          <Segmented
            value={settings.webrtcMode}
            disabled={saving}
            onChange={(v) => apply({ webrtcMode: v })}
            options={[
              { value: "proxy", label: i18n.t('settings.k030') },
              { value: "reject", label: i18n.t('settings.k031') },
              { value: "off", label: i18n.t('settings.k032') },
            ]}
          />
        </SettingCard>

        <SettingCard
          icon={<Globe className="h-[18px] w-[18px]" />}
          title="DNS"
          desc={i18n.t('settings.k033')}
        >
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-sm">{i18n.t('settings.k034')}</span>
              <Segmented
                value={settings.dnsFallback}
                disabled={saving}
                onChange={(v) => apply({ dnsFallback: v })}
                options={[
                  { value: "foreign", label: i18n.t('settings.k035') },
                  { value: "domestic", label: i18n.t('settings.k036') },
                ]}
              />
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm">respect-rules</span>
              <EnabledSwitch
                enabled={settings.dnsRespectRules}
                disabled={saving}
                onToggle={() =>
                  apply({ dnsRespectRules: !settings.dnsRespectRules })
                }
              />
            </div>
          </div>
        </SettingCard>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
      <SettingCard
        icon={<Share2 className="h-[18px] w-[18px]" />}
        title={i18n.t('settings.k037')}
        desc={i18n.t('settings.k038')}
      >
        <div className="divide-y">
          <Row label={i18n.t('settings.k039')}>
            <Segmented
              value={settings.linkFormat}
              disabled={saving}
              onChange={(v) => apply({ linkFormat: v })}
              options={[
                { value: "clash", label: "Clash" },
                { value: "singbox", label: "sing-box JSON" },
              ]}
            />
          </Row>
          <Row label={i18n.t('settings.k040')} hint={i18n.t('settings.k041')}>
            <Input
              value={publicHostDraft}
              disabled={saving}
              onChange={(e) => setPublicHostDraft(e.target.value)}
              onBlur={() => {
                const v = publicHostDraft.trim();
                if (v !== settings.publicHost) apply({ publicHost: v });
              }}
              placeholder={i18n.t('settings.k042')}
              className="max-w-xs font-mono text-[13px]"
            />
          </Row>
          <Row label={i18n.t('settings.k043')} hint={i18n.t('settings.k044')}>
            <EnabledSwitch
              enabled={settings.skipCertVerify}
              disabled={saving}
              onToggle={() =>
                apply({ skipCertVerify: !settings.skipCertVerify })
              }
            />
          </Row>
          <Row label={i18n.t('settings.k045')} hint={i18n.t('settings.k046')}>
            <EnabledSwitch
              enabled={settings.showUserinfo}
              disabled={saving}
              onToggle={() =>
                apply({ showUserinfo: !settings.showUserinfo })
              }
            />
          </Row>
          <Row label={i18n.t('settings.k047')} hint={i18n.t('settings.k048')}>
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={0}
                max={720}
                value={intervalDraft}
                disabled={saving}
                onChange={(e) => setIntervalDraft(e.target.value)}
                onBlur={commitInterval}
                className="w-24"
              />
              <span className="text-sm text-muted-foreground">
                {i18n.t('settings.k060')}
              </span>
            </div>
          </Row>
        </div>
      </SettingCard>

      <SettingCard
        icon={<Type className="h-[18px] w-[18px]" />}
        title={i18n.t('settings.k049')}
        desc={i18n.t('settings.k050')}
      >
        <div className="divide-y">
          <Row label={i18n.t('settings.language')} hint={i18n.t('settings.languageHint')}>
            <Select
              value={i18n.language === "en" ? "en" : "zh"}
              onValueChange={(v) => void setLanguage(v as SupportedLanguage)}
            >
              <SelectTrigger className="w-full max-w-[280px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="zh">{i18n.t('settings.languageZh')}</SelectItem>
                <SelectItem value="en">{i18n.t('settings.languageEn')}</SelectItem>
              </SelectContent>
            </Select>
          </Row>
          <Row label={i18n.t('settings.k051')} hint={i18n.t('settings.k052')}>
            <EnabledSwitch
              enabled={settings.textSelectable}
              disabled={saving}
              onToggle={() =>
                apply({ textSelectable: !settings.textSelectable })
              }
            />
          </Row>
          <Row label={i18n.t('settings.k053')} hint={i18n.t('settings.k054')}>
            <Select
              value={settings.timezone}
              disabled={saving}
              onValueChange={(v) => apply({ timezone: v })}
            >
              <SelectTrigger className="w-full max-w-[280px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {timezoneOptions().some(
                  (o) => o.value === settings.timezone,
                ) ? null : (
                  <SelectItem value={settings.timezone}>
                    {settings.timezone}
                  </SelectItem>
                )}
                {timezoneOptions().map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
          <Row label={i18n.t('settings.k055')} hint={i18n.t('settings.k056')}>
            <EnabledSwitch
              enabled={settings.showWarnings}
              disabled={saving}
              onToggle={() =>
                apply({ showWarnings: !settings.showWarnings })
              }
            />
          </Row>
          <Row
            label={i18n.t('settings.k057')}
            hint={i18n.t('settings.k058')}
          >
            <div className="flex items-center gap-2">
              <Input
                type="number"
                min={1}
                max={60}
                value={dashDraft}
                disabled={saving}
                onChange={(e) => setDashDraft(e.target.value)}
                onBlur={commitDashInterval}
                className="w-24"
              />
              <span className="text-sm text-muted-foreground">{i18n.t('settings.k059')}</span>
            </div>
          </Row>
        </div>
      </SettingCard>
      </div>

      <p className="text-xs text-muted-foreground">
        {i18n.t('settings.k061')}
      </p>
    </div>
  );
}
