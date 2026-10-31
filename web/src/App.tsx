import { useCallback, useEffect, useState } from "react";

import AppLayout, { ROUTES, type RouteKey } from "@/components/layout/AppLayout";
import { Toaster } from "@/components/ui/sonner";
import { api, onConfigChanged } from "@/lib/api";
import { useTheme } from "@/lib/theme";
import CertificatesPage from "@/pages/CertificatesPage";
import DashboardPage from "@/pages/DashboardPage";
import InboundsPage from "@/pages/InboundsPage";
import LoginPage from "@/pages/LoginPage";
import LogsPage from "@/pages/LogsPage";
import NodesPage from "@/pages/NodesPage";
import SecurityPage from "@/pages/SecurityPage";
import SettingsPage from "@/pages/SettingsPage";
import SubscriptionsPage from "@/pages/SubscriptionsPage";

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

  // Panel-wide text selection: server setting, default off. Refetched
  // on login and whenever any settings change lands (the settings
  // page saves through the same request layer that emits the event).
  // The yellow-warnings switch rides the same fetch (default on).
  const [textSelectable, setTextSelectable] = useState(false);
  const [showWarnings, setShowWarnings] = useState(true);
  useEffect(() => {
    if (auth.phase !== "authed") return;
    let cancelled = false;
    const fetchSelection = () => {
      api.settings
        .get()
        .then((v) => {
          if (!cancelled) {
            setTextSelectable(v.textSelectable);
            setShowWarnings(v.showWarnings);
          }
        })
        .catch(() => {
          /* defaults stand on failure */
        });
    };
    fetchSelection();
    const off = onConfigChanged(fetchSelection);
    return () => {
      cancelled = true;
      off();
    };
  }, [auth.phase]);

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
    return (
      <>
        <LoginPage onLogin={handleLogin} />
        <Toaster theme={theme} />
      </>
    );
  }

  return (
    <>
    <AppLayout
      route={route}
      onNavigate={navigate}
      username={auth.username}
      theme={theme}
      onToggleTheme={toggleTheme}
      onLogout={handleLogout}
      onRefresh={handleRefresh}
      textSelectable={textSelectable}
    >
      {route === "dashboard" && (
        <DashboardPage refreshSignal={refreshNonce} />
      )}
      {route === "subscriptions" && (
        <SubscriptionsPage refreshSignal={refreshNonce} />
      )}
      {route === "inbounds" && (
        <InboundsPage refreshSignal={refreshNonce} showWarnings={showWarnings} />
      )}
      {route === "nodes" && <NodesPage refreshSignal={refreshNonce} />}
      {route === "certificates" && (
        <CertificatesPage
          refreshSignal={refreshNonce}
          showWarnings={showWarnings}
        />
      )}
      {route === "settings" && (
        <SettingsPage refreshSignal={refreshNonce} />
      )}
      {route === "logs" && <LogsPage refreshSignal={refreshNonce} />}
      {route === "security" && (
        <SecurityPage
          username={auth.username}
          onUsernameChanged={(u) => {
            // Keep the login page's remembered username in step, or
            // it would prefill the old name after logout.
            localStorage.setItem("singbox-ui-username", u);
            setAuth({ phase: "authed", username: u });
          }}
        />
      )}
    </AppLayout>
    <Toaster theme={theme} />
    </>
  );
}
