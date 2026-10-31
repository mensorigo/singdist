import { useState, type ReactNode } from "react";
import { Eye, EyeOff, User } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api, ApiError } from "@/lib/api";
import i18n from "@/lib/i18n";

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
    <section className="rounded-[18px] border bg-card px-5 py-4 text-card-foreground shadow-sm">
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
          aria-label={show ? i18n.t('security.k001') : i18n.t('security.k002')}
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
    if (err.code === "wrong_password") return i18n.t('security.k003');
    if (err.code === "username_taken") return i18n.t('security.k004');
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

  const name = newUsername.trim();
  const nameChanged = name !== "" && name !== username;
  const passChanged = nextPassword !== "";
  const hasChange = nameChanged || passChanged;

  const submitAccount = async () => {
    if (!currentPassword || !hasChange || saving) return;
    if (newUsername.trim() === "") {
      setFormError(i18n.t('security.k005'));
      return;
    }
    if (/\s/.test(newUsername)) {
      setFormError(i18n.t('security.k006'));
      return;
    }
    if (passChanged) {
      if (nextPassword.length < 8 || nextPassword.length > 72) {
        setFormError(i18n.t('security.k007'));
        return;
      }
      if (nextPassword === currentPassword) {
        setFormError(i18n.t('security.k008'));
        return;
      }
      if (nextPassword !== confirmPassword) {
        setFormError(i18n.t('security.k009'));
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
        toast.success(i18n.t('security.k010'));
      } else if (nameChanged) {
        toast.success(i18n.t('security.k011'));
      } else {
        toast.success(i18n.t('security.k012'));
      }
      onUsernameChanged(res.username);
      setNewUsername(res.username);
      setCurrentPassword("");
      setNextPassword("");
      setConfirmPassword("");
    } catch (err) {
      setFormError(authErrorText(err, i18n.t('security.k013')));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-3">
      <SectionCard
        icon={<User className="h-[18px] w-[18px]" />}
        title={i18n.t('security.k014')}
        desc={i18n.t('security.k015')}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void submitAccount();
          }}
          className="space-y-3"
        >
          <div>
            <p className="mb-1.5 text-sm">{i18n.t('security.k016')}</p>
            <Input
              value={newUsername}
              onChange={(e) => setNewUsername(e.target.value)}
              placeholder={i18n.t('security.k017')}
              maxLength={64}
              disabled={saving}
              autoComplete="username"
              className="max-w-xs"
            />
          </div>
          <PasswordField
            label={i18n.t('security.k018')}
            value={currentPassword}
            onChange={setCurrentPassword}
            placeholder={i18n.t('security.k019')}
            disabled={saving}
            autoComplete="current-password"
          />
          <PasswordField
            label={i18n.t('security.k020')}
            value={nextPassword}
            onChange={setNextPassword}
            placeholder={i18n.t('security.k021')}
            disabled={saving}
            autoComplete="new-password"
          />
          {passChanged && (
            <PasswordField
              label={i18n.t('security.k022')}
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
            {i18n.t('security.k023')}
          </Button>
        </form>
      </SectionCard>
    </div>
  );
}
