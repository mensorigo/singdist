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

// Curated IANA zones for the panel timezone dropdown. The stored value
// is always offered as well, so a zone set elsewhere never renders blank.
const TIMEZONE_OPTIONS: { value: string; label: string }[] = [
  { value: "Asia/Shanghai", label: "上海 (Asia/Shanghai)" },
  { value: "Asia/Taipei", label: "台北 (Asia/Taipei)" },
  { value: "Asia/Hong_Kong", label: "香港 (Asia/Hong_Kong)" },
  { value: "Asia/Tokyo", label: "东京 (Asia/Tokyo)" },
  { value: "Asia/Seoul", label: "首尔 (Asia/Seoul)" },
  { value: "Asia/Singapore", label: "新加坡 (Asia/Singapore)" },
  { value: "Asia/Bangkok", label: "曼谷 (Asia/Bangkok)" },
  { value: "Asia/Dubai", label: "迪拜 (Asia/Dubai)" },
  { value: "Asia/Kolkata", label: "加尔各答 (Asia/Kolkata)" },
  { value: "Europe/London", label: "伦敦 (Europe/London)" },
  { value: "Europe/Paris", label: "巴黎 (Europe/Paris)" },
  { value: "Europe/Berlin", label: "柏林 (Europe/Berlin)" },
  { value: "Europe/Moscow", label: "莫斯科 (Europe/Moscow)" },
  { value: "Africa/Cairo", label: "开罗 (Africa/Cairo)" },
  { value: "America/New_York", label: "纽约 (America/New_York)" },
  { value: "America/Chicago", label: "芝加哥 (America/Chicago)" },
  { value: "America/Denver", label: "丹佛 (America/Denver)" },
  { value: "America/Los_Angeles", label: "洛杉矶 (America/Los_Angeles)" },
  { value: "America/Sao_Paulo", label: "圣保罗 (America/Sao_Paulo)" },
  { value: "Australia/Sydney", label: "悉尼 (Australia/Sydney)" },
  { value: "Pacific/Auckland", label: "奥克兰 (Pacific/Auckland)" },
  { value: "UTC", label: "协调世界时 (UTC)" },
];

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
        toast.success("设置已保存");
      } catch (err) {
        setSettings(prev);
        toast.error(
          err instanceof ApiError ? err.message : "保存失败，请重试",
        );
      } finally {
        setSaving(false);
      }
    })();
  };

  if (!settings) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? "暂时无法获取设置，请稍后刷新重试" : "数据加载中…"}
      </p>
    );
  }

  const commitInterval = () => {
    const n = Number(intervalDraft);
    if (!Number.isInteger(n) || n < 0 || n > 720) {
      toast.error("自动更新间隔需为 0–720 的整数（小时）");
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
      toast.error("数据刷新频率需为 1–60 的整数（秒）");
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
          desc="STUN 打洞流量的走向。"
        >
          <Segmented
            value={settings.webrtcMode}
            disabled={saving}
            onChange={(v) => apply({ webrtcMode: v })}
            options={[
              { value: "proxy", label: "走代理" },
              { value: "reject", label: "拒绝" },
              { value: "off", label: "关闭" },
            ]}
          />
        </SettingCard>

        <SettingCard
          icon={<Globe className="h-[18px] w-[18px]" />}
          title="DNS"
          desc="订阅 DNS 的兜底解析。"
        >
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-sm">兜底解析</span>
              <Segmented
                value={settings.dnsFallback}
                disabled={saving}
                onChange={(v) => apply({ dnsFallback: v })}
                options={[
                  { value: "foreign", label: "国外（Cloudflare / Google）" },
                  { value: "domestic", label: "国内（阿里 / 腾讯）" },
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
        title="订阅分发"
        desc="生成订阅内容时使用的全局选项。"
      >
        <div className="divide-y">
          <Row label="默认格式">
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
          <Row label="本机对外地址" hint="只对本机节点生效：本机节点在订阅里的服务器地址，节点用自己的对外地址">
            <Input
              value={publicHostDraft}
              disabled={saving}
              onChange={(e) => setPublicHostDraft(e.target.value)}
              onBlur={() => {
                const v = publicHostDraft.trim();
                if (v !== settings.publicHost) apply({ publicHost: v });
              }}
              placeholder="留空自动取面板域名"
              className="max-w-xs font-mono text-[13px]"
            />
          </Row>
          <Row label="跳过证书校验" hint="自签证书时客户端需要">
            <EnabledSwitch
              enabled={settings.skipCertVerify}
              disabled={saving}
              onToggle={() =>
                apply({ skipCertVerify: !settings.skipCertVerify })
              }
            />
          </Row>
          <Row label="订阅信息" hint="在客户端显示流量与到期">
            <EnabledSwitch
              enabled={settings.showUserinfo}
              disabled={saving}
              onToggle={() =>
                apply({ showUserinfo: !settings.showUserinfo })
              }
            />
          </Row>
          <Row label="自动更新间隔" hint="客户端拉取订阅的周期">
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
                小时（0 = 不指定）
              </span>
            </div>
          </Row>
        </div>
      </SettingCard>

      <SettingCard
        icon={<Type className="h-[18px] w-[18px]" />}
        title="界面"
        desc="面板自身的显示行为。"
      >
        <div className="divide-y">
          <Row label="允许选中文本" hint="关闭后页面文本长按无法选中">
            <EnabledSwitch
              enabled={settings.textSelectable}
              disabled={saving}
              onToggle={() =>
                apply({ textSelectable: !settings.textSelectable })
              }
            />
          </Row>
          <Row label="时区" hint="统一面板时区设置">
            <Select
              value={settings.timezone}
              disabled={saving}
              onValueChange={(v) => apply({ timezone: v })}
            >
              <SelectTrigger className="w-full max-w-[280px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TIMEZONE_OPTIONS.some(
                  (o) => o.value === settings.timezone,
                ) ? null : (
                  <SelectItem value={settings.timezone}>
                    {settings.timezone}
                  </SelectItem>
                )}
                {TIMEZONE_OPTIONS.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {o.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
          <Row label="显示黄色警告" hint="关闭后隐藏黄色警告">
            <EnabledSwitch
              enabled={settings.showWarnings}
              disabled={saving}
              onToggle={() =>
                apply({ showWarnings: !settings.showWarnings })
              }
            />
          </Row>
          <Row
            label="数据刷新频率"
            hint="仪表盘与在线统计的刷新间隔"
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
              <span className="text-sm text-muted-foreground">秒</span>
            </div>
          </Row>
        </div>
      </SettingCard>
      </div>

      <p className="text-xs text-muted-foreground">
        这些选项保存后即时生效。
      </p>
    </div>
  );
}
