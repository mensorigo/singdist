import { useCallback, useEffect, useState } from "react";

import AppLayout, { ROUTES, type RouteKey } from "@/components/layout/AppLayout";
import { api } from "@/lib/api";
import { useTheme } from "@/lib/theme";
import CertificatesPage from "@/pages/CertificatesPage";
import DashboardPage from "@/pages/DashboardPage";
import LoginPage from "@/pages/LoginPage";
import PlaceholderPage from "@/pages/PlaceholderPage";

function routeFromPath(path: string): RouteKey {
  const found = ROUTES.find((r) => r.path === path);
  return found ? found.key : "dashboard";
}

function pathForRoute(key: RouteKey): string {
  return ROUTES.find((r) => r.key === key)?.path ?? "/dashboard";
}

type AuthState =
  | { phase: "loading" }
  | { phase: "anon" }
  | { phase: "authed"; username: string };

export default function App() {
  const { theme, toggleTheme } = useTheme();
  const [auth, setAuth] = useState<AuthState>({ phase: "loading" });
  const [route, setRoute] = useState<RouteKey>(() =>
    routeFromPath(window.location.pathname),
  );
  // Bumped by the top-bar refresh button; pages watch this signal and
  // refetch their data in place (no remount: scroll position, tab choices
  // and form contents all survive a refresh).
  const [refreshNonce, setRefreshNonce] = useState(0);
  const handleRefresh = useCallback(() => setRefreshNonce((n) => n + 1), []);

  // Session check on load: the server is the source of truth.
  useEffect(() => {
    let cancelled = false;
    api
      .status()
      .then((s) => {
        if (cancelled) return;
        if (s.authenticated && s.username) {
          setAuth({ phase: "authed", username: s.username });
        } else {
          setAuth({ phase: "anon" });
        }
      })
      .catch(() => {
        if (!cancelled) setAuth({ phase: "anon" });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const onPop = () => setRoute(routeFromPath(window.location.pathname));
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const navigate = useCallback((key: RouteKey) => {
    setRoute(key);
    window.history.pushState(null, "", pathForRoute(key));
  }, []);

  const handleLogin = useCallback(
    (username: string) => {
      setAuth({ phase: "authed", username });
      navigate("dashboard");
    },
    [navigate],
  );

  const handleLogout = useCallback(async () => {
    try {
      await api.logout();
    } catch {
      /* session may already be gone; log out locally regardless */
    }
    setAuth({ phase: "anon" });
    window.history.pushState(null, "", "/login");
  }, []);

  if (auth.phase === "loading") {
    return (
      <div className="flex min-h-svh items-center justify-center text-sm text-muted-foreground">
        加载中…
      </div>
    );
  }

  if (auth.phase === "anon") {
    return <LoginPage onLogin={handleLogin} />;
  }

  return (
    <AppLayout
      route={route}
      onNavigate={navigate}
      username={auth.username}
      theme={theme}
      onToggleTheme={toggleTheme}
      onLogout={handleLogout}
      onRefresh={handleRefresh}
    >
      {route === "dashboard" && (
        <DashboardPage refreshSignal={refreshNonce} />
      )}
      {route === "subscriptions" && <PlaceholderPage title="订阅管理" />}
      {route === "inbounds" && <PlaceholderPage title="入站" />}
      {route === "certificates" && (
        <CertificatesPage refreshSignal={refreshNonce} />
      )}
      {route === "settings" && <PlaceholderPage title="设置" />}
    </AppLayout>
  );
}
