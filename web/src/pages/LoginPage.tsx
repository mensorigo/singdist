import { KeyRound } from "lucide-react";
import { useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, ApiError } from "@/lib/api";
import i18n from "@/lib/i18n";

interface Props {
  onLogin: (username: string) => void;
}

export default function LoginPage({ onLogin }: Props) {
  // Remember the last login username in this browser only: the
  // login page is unauthenticated, so it must never ask the server
  // for the current admin name (that would leak it to visitors).
  // A hardcoded default would go stale the moment the admin renames
  // their account, which is exactly what happened with "singbox-ui".
  const [username, setUsername] = useState(
    () => localStorage.getItem("singbox-ui-username") ?? "",
  );
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const res = await api.login(username.trim(), password);
      localStorage.setItem("singbox-ui-username", res.username);
      onLogin(res.username);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === "rate_limited") {
          setError(i18n.t('login.k001'));
        } else if (err.code === "invalid_credentials") {
          setError(i18n.t('login.k002'));
        } else {
          setError(i18n.t('login.k003', { value: err.message }));
        }
      } else {
        setError(i18n.t('login.k004'));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <div className="mb-2 flex h-12 w-12 items-center justify-center rounded-full bg-primary text-primary-foreground">
            <KeyRound className="h-6 w-6" />
          </div>
          <CardTitle className="text-[22px] font-semibold tracking-tight">
            singbox-ui
          </CardTitle>
          <CardDescription>{i18n.t('login.k005')}</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username">{i18n.t('login.k006')}</Label>
              <Input
                id="username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">{i18n.t('login.k007')}</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            {error && (
              <div
                role="alert"
                className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive"
              >
                {error}
              </div>
            )}
            <Button type="submit" className="w-full" disabled={busy}>
              {busy ? i18n.t('login.k008') : i18n.t('login.k009')}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
