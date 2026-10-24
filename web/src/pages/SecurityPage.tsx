import { useState, type ReactNode } from "react";
import { Eye, EyeOff, MonitorSmartphone, User } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api, ApiError } from "@/lib/api";

function SectionCard({
  icon,
  title,
  desc,
  children,
}: {
  icon: ReactNode;
  title: string;
  desc?: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-xl border bg-card px-5 py-4 text-card-foreground shadow-sm">
      <div className="flex items-center gap-2.5">
        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          {icon}
        </span>
        <p className="text-base font-semibold">{title}</p>
      </div>
      {desc && (
        <p className="mt-2 text-[13px] text-muted-foreground">{desc}</p>
      )}
      <div className="mt-4 space-y-3">{children}</div>
    </section>
  );
}

function PasswordField({
  label,
  value,
  onChange,
  placeholder,
  disabled,
  autoComplete,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  disabled?: boolean;
  autoComplete?: string;
}) {
  const [show, setShow] = useState(false);
  return (
    <div>
      <p className="mb-1.5 text-sm">{label}</p>
      <div className="relative max-w-xs">
        <Input
          type={show ? "text" : "password"}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          disabled={disabled}
          autoComplete={autoComplete}
          className="pr-10"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={() => setShow((s) => !s)}
          disabled={disabled}
          aria-label={show ? "隐藏密码" : "显示密码"}
          className="absolute right-1 top-1/2 h-7 w-7 -translate-y-1/2 rounded-lg [&_svg]:size-4"
        >
          {show ? <EyeOff /> : <Eye />}
        </Button>
      </div>
    </div>
  );
}

function authErrorText(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    if (err.code === "wrong_password") return "当前密码不正确";
    if (err.code === "username_taken") return "用户名已被占用";
    if (err.code === "invalid_request") return err.message || fallback;
    return err.message || fallback;
  }
  return fallback;
}

export default function SecurityPage({
  username,
  onUsernameChanged,
}: {
  username: string;
  onUsernameChanged: (username: string) => void;
}) {
  const [newUsername, setNewUsername] = useState(username);
  const [currentPassword, setCurrentPassword] = useState("");
  const [nextPassword, setNextPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [formError, setFormError] = useState("");
  const [saving, setSaving] = useState(false);

  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const [revoking, setRevoking] = useState(false);

  const name = newUsername.trim();
  const nameChanged = name !== "" && name !== username;
  const passChanged = nextPassword !== "";
  const hasChange = nameChanged || passChanged;

  const submitAccount = async () => {
    if (!currentPassword || !hasChange || saving) return;
    if (newUsername.trim() === "") {
      setFormError("用户名不能为空");
      return;
    }
    if (/\s/.test(newUsername)) {
      setFormError("用户名不能含空格");
      return;
    }
    if (passChanged) {
      if (nextPassword.length < 8 || nextPassword.length > 72) {
        setFormError("新密码需要 8–72 个字符");
        return;
      }
      if (nextPassword === currentPassword) {
        setFormError("新密码不能与当前密码相同");
        return;
      }
      if (nextPassword !== confirmPassword) {
        setFormError("两次输入的新密码不一致");
        return;
      }
    }
    setFormError("");
    setSaving(true);
    try {
      const res = await api.changeAccount({
        ...(nameChanged ? { username: name } : {}),
        currentPassword,
        ...(passChanged ? { newPassword: nextPassword } : {}),
      });
      if (nameChanged && passChanged) {
        toast.success("用户名和密码已修改，其他设备的登录已失效");
      } else if (nameChanged) {
        toast.success("用户名已修改");
      } else {
        toast.success("密码已修改，其他设备的登录已失效");
      }
      onUsernameChanged(res.username);
      setNewUsername(res.username);
      setCurrentPassword("");
      setNextPassword("");
      setConfirmPassword("");
    } catch (err) {
      setFormError(authErrorText(err, "保存失败，请重试"));
    } finally {
      setSaving(false);
    }
  };

  const runRevoke = async () => {
    setConfirmRevoke(false);
    setRevoking(true);
    try {
      const res = await api.revokeOtherSessions();
      toast.success(
        res.revoked > 0
          ? `已退出 ${res.revoked} 个其他会话`
          : "没有其他会话需要退出",
      );
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "操作失败，请重试");
    } finally {
      setRevoking(false);
    }
  };

  return (
    <div className="space-y-3">
      <SectionCard
        icon={<User className="h-[18px] w-[18px]" />}
        title="账号安全"
        desc="用户名和密码可以在这里一起改；只填要改的那一项，不填则保持不变。提交时验证一次当前密码。"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void submitAccount();
          }}
          className="space-y-3"
        >
          <div>
            <p className="mb-1.5 text-sm">用户名</p>
            <Input
              value={newUsername}
              onChange={(e) => setNewUsername(e.target.value)}
              placeholder="与当前相同则不改名"
              maxLength={64}
              disabled={saving}
              autoComplete="username"
              className="max-w-xs"
            />
          </div>
          <PasswordField
            label="当前密码"
            value={currentPassword}
            onChange={setCurrentPassword}
            placeholder="验证身份"
            disabled={saving}
            autoComplete="current-password"
          />
          <PasswordField
            label="新密码"
            value={nextPassword}
            onChange={setNextPassword}
            placeholder="不改请留空，8–72 个字符"
            disabled={saving}
            autoComplete="new-password"
          />
          {passChanged && (
            <PasswordField
              label="确认新密码"
              value={confirmPassword}
              onChange={setConfirmPassword}
              disabled={saving}
              autoComplete="new-password"
            />
          )}
          {formError && (
            <p className="text-sm text-destructive">{formError}</p>
          )}
          <Button
            type="submit"
            disabled={saving || !currentPassword || !hasChange}
          >
            保存修改
          </Button>
        </form>
      </SectionCard>

      <SectionCard
        icon={<MonitorSmartphone className="h-[18px] w-[18px]" />}
        title="会话"
        desc="怀疑账号被他人使用时，先修改密码，再退出其他设备。"
      >
        <Button
          variant="outline"
          disabled={revoking}
          onClick={() => setConfirmRevoke(true)}
        >
          退出其他设备
        </Button>
      </SectionCard>

      <ConfirmDialog
        open={confirmRevoke}
        onOpenChange={setConfirmRevoke}
        title="退出其他设备"
        description="确定要退出除当前设备外的所有登录吗？当前设备不受影响。"
        confirmLabel="退出其他设备"
        onConfirm={runRevoke}
      />
    </div>
  );
}
