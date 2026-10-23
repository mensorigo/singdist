import {
  ChevronsLeft,
  LayoutGrid,
  LogOut,
  Moon,
  RefreshCw,
  Server,
  Settings as SettingsIcon,
  Share2,
  ShieldCheck,
  Sun,
  Users,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { Theme } from "@/lib/theme";

export type RouteKey =
  | "dashboard"
  | "subscriptions"
  | "inbounds"
  | "certificates"
  | "settings";

export const ROUTES: { key: RouteKey; path: string; label: string }[] = [
  { key: "dashboard", path: "/dashboard", label: "仪表盘" },
  { key: "subscriptions", path: "/subscriptions", label: "订阅管理" },
  { key: "inbounds", path: "/inbounds", label: "入站" },
  { key: "certificates", path: "/certificates", label: "证书管理" },
  { key: "settings", path: "/settings", label: "设置" },
];

const NAV_ICONS: Record<RouteKey, ReactNode> = {
  dashboard: <LayoutGrid className="h-[18px] w-[18px] shrink-0" />,
  subscriptions: <Users className="h-[18px] w-[18px] shrink-0" />,
  inbounds: <Server className="h-[18px] w-[18px] shrink-0" />,
  certificates: <ShieldCheck className="h-[18px] w-[18px] shrink-0" />,
  settings: <SettingsIcon className="h-[18px] w-[18px] shrink-0" />,
};

const SIDEBAR_STORAGE_KEY = "subdist-sidebar-collapsed";

function initialCollapsed(): boolean {
  try {
    const saved = localStorage.getItem(SIDEBAR_STORAGE_KEY);
    if (saved === "1") return true;
    if (saved === "0") return false;
  } catch {
    /* storage unavailable */
  }
  return true; // icon rail by default, like the old panel
}

interface Props {
  route: RouteKey;
  onNavigate: (key: RouteKey) => void;
  username: string;
  theme: Theme;
  onToggleTheme: () => void;
  onLogout: () => void;
  onRefresh: () => void;
  children: ReactNode;
}

export default function AppLayout({
  route,
  onNavigate,
  username,
  theme,
  onToggleTheme,
  onLogout,
  onRefresh,
  children,
}: Props) {
  const [collapsed, setCollapsed] = useState<boolean>(initialCollapsed);
  const [refreshTurns, setRefreshTurns] = useState(0);

  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_STORAGE_KEY, collapsed ? "1" : "0");
    } catch {
      /* storage unavailable */
    }
  }, [collapsed]);

  const current = ROUTES.find((r) => r.key === route);

  // Refresh icon: accumulate full turns instead of Tailwind's linear
  // animate-spin toggled by a timer (which visibly snapped mid-rotation).
  // Each click eases one whole 360° turn and always settles back at rest;
  // rapid clicks keep turning forward without a jump.
  const handleRefresh = () => {
    setRefreshTurns((t) => t + 1);
    onRefresh();
  };

  // One fixed-width inner column for both states: the aside only animates its
  // own width (clipping the rest), so icons sit in a fixed slot whose centre
  // (x = 35px) never moves. Labels move asymmetrically for a natural feel:
  // expanding = delayed fade-in with a slight settle slide; collapsing =
  // fast fade-out so text never lingers half-clipped.
  const labelCls = cn(
    "whitespace-nowrap transition-[opacity,transform] ease-out",
    collapsed
      ? "-translate-x-1 opacity-0 duration-200"
      : "translate-x-0 opacity-100 delay-100 duration-[250ms]",
  );

  // Brand text gets NO slide: a fractional-pixel transform settling at the
  // end of the transition makes the browser re-rasterise the text layer,
  // which reads as a tiny snap on these two lines. Opacity-only plus a
  // pinned compositing layer (will-change) keeps it perfectly still.
  const brandLabelCls = cn(
    "min-w-0 leading-tight transition-opacity ease-out [will-change:opacity]",
    collapsed
      ? "opacity-0 duration-200"
      : "opacity-100 delay-100 duration-[250ms]",
  );

  const sidebarBody = (
    <div className="flex h-full w-[270px] flex-col">
      {/* Brand: the logo tile itself is the expand/collapse toggle. Kept in
          the nav's visual language (muted tile, nav-size title) so the header
          reads as part of the same column instead of shouting over it. */}
      <div className="flex items-center py-4">
        <button
          onClick={() => setCollapsed((v) => !v)}
          title={collapsed ? "展开侧边栏" : "折叠侧边栏"}
          aria-label={collapsed ? "展开侧边栏" : "折叠侧边栏"}
          className="flex w-[70px] shrink-0 items-center justify-center"
        >
          <span className="flex h-11 w-11 items-center justify-center rounded-lg bg-muted text-foreground/80 transition-colors duration-300 hover:bg-accent hover:text-foreground">
            <Share2 className="h-[22px] w-[22px]" />
          </span>
        </button>
        <div className={brandLabelCls}>
          <p className="truncate text-[19px] font-semibold leading-tight tracking-tight">
            SingDist
          </p>
        </div>
        {/* Collapse chevron: kept for the expanded state only (fades out and
            becomes unclickable when collapsed; the logo expands it back) */}
        <button
          onClick={() => setCollapsed(true)}
          title="折叠侧边栏"
          aria-label="折叠侧边栏"
          tabIndex={collapsed ? -1 : 0}
          className={cn(
            "ml-auto mr-3 flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-opacity ease-out hover:bg-accent hover:text-foreground",
            collapsed
              ? "pointer-events-none opacity-0 duration-200"
              : "opacity-100 delay-100 duration-[250ms]",
          )}
        >
          <ChevronsLeft className="h-[18px] w-[18px]" />
        </button>
      </div>

      {/* Nav: 44px rows, icon in a fixed 46px slot, label fades with the rail.
          The row background is its own pill layer whose width animates
          (46px when collapsed, full row when expanded) with the same
          350ms curve as the rail, instead of being hard-clipped. */}
      <nav className="flex-1 space-y-1 px-3">
        {ROUTES.map((r) => (
          <button
            key={r.key}
            onClick={() => onNavigate(r.key)}
            title={collapsed ? r.label : undefined}
            aria-label={r.label}
            className={cn(
              "group relative flex h-11 w-full items-center rounded-lg text-[15px] transition-colors",
              route === r.key
                ? "font-medium text-primary-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            <span
              aria-hidden
              style={{ width: collapsed ? 46 : "100%" }}
              className={cn(
                "absolute inset-y-0 left-0 rounded-lg transition-[width] duration-[350ms] ease-[cubic-bezier(0.22,1,0.36,1)]",
                route === r.key
                  ? "bg-primary"
                  : "bg-transparent group-hover:bg-accent/60",
              )}
            />
            <span className="relative z-10 flex w-[46px] shrink-0 items-center justify-center">
              {NAV_ICONS[r.key]}
            </span>
            <span className={cn("relative z-10", labelCls)}>{r.label}</span>
          </button>
        ))}
      </nav>

      {/* Bottom-left logout (original full-row button). The divider above it
          is inset at both ends: left edge pinned at 20px, width animates
          with the rail — full inner width when expanded, a short dash
          centred on the icon column (20..50px) when collapsed. */}
      <div>
        <div className="px-5">
          <div
            aria-hidden
            style={{ width: collapsed ? 30 : "100%" }}
            className="h-px bg-border transition-[width] duration-[350ms] ease-[cubic-bezier(0.22,1,0.36,1)]"
          />
        </div>
        <div className="px-3 py-3">
          <button
            onClick={onLogout}
            title={collapsed ? `退出登录（${username}）` : "退出登录"}
            aria-label="退出登录"
            className="group relative flex h-11 w-full items-center rounded-lg text-[15px] text-muted-foreground transition-colors hover:text-foreground"
          >
            <span
              aria-hidden
              style={{ width: collapsed ? 46 : "100%" }}
              className="absolute inset-y-0 left-0 rounded-lg bg-transparent transition-[width] duration-[350ms] ease-[cubic-bezier(0.22,1,0.36,1)] group-hover:bg-accent/60"
            />
            <span className="relative z-10 flex w-[46px] shrink-0 items-center justify-center">
              <LogOut className="h-[18px] w-[18px] shrink-0" />
            </span>
            <span className={cn("relative z-10", labelCls)}>退出登录</span>
          </button>
        </div>
      </div>
    </div>
  );

  return (
    <div className="flex min-h-svh bg-zinc-50 dark:bg-background">
      {/* Sidebar: md and up, collapsible to an icon rail */}
      <aside
        className={cn(
          "sticky top-0 hidden h-svh shrink-0 overflow-hidden border-r bg-card transition-[width] duration-[350ms] ease-[cubic-bezier(0.22,1,0.36,1)] md:block",
          collapsed ? "w-[70px]" : "w-[270px]",
        )}
      >
        {sidebarBody}
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Top bar: category on the left, refresh + theme on the right */}
        <header className="sticky top-0 z-10 border-b bg-card">
          <div className="flex h-14 items-center gap-2 px-4 md:px-6">
            <span className="text-lg font-semibold">
              {current?.label ?? ""}
            </span>

            <div className="ml-auto flex items-center gap-1.5">
              <Button
                variant="ghost"
                size="icon"
                className="h-11 w-11 [&_svg]:size-6"
                onClick={handleRefresh}
                title="刷新"
                aria-label="刷新"
              >
                <RefreshCw
                  strokeWidth={1.75}
                  style={{ transform: `rotate(${refreshTurns * 360}deg)` }}
                  className="transition-transform duration-1000 ease-[cubic-bezier(0.22,1,0.36,1)]"
                />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-11 w-11 [&_svg]:size-6"
                onClick={onToggleTheme}
                title={theme === "dark" ? "切换到浅色" : "切换到暗色"}
                aria-label="切换主题"
              >
                {theme === "dark" ? (
                  <Sun strokeWidth={1.75} />
                ) : (
                  <Moon strokeWidth={1.75} />
                )}
              </Button>
              {/* No sidebar on small screens: keep logout reachable here */}
              <Button
                variant="ghost"
                size="icon"
                className="h-11 w-11 [&_svg]:size-6 md:hidden"
                onClick={onLogout}
                title="退出登录"
                aria-label="退出登录"
              >
                <LogOut strokeWidth={1.75} />
              </Button>
            </div>
          </div>

          {/* Mobile nav: small screens only */}
          <nav className="flex gap-1 overflow-x-auto px-4 pb-2 sm:px-6 md:hidden">
            {ROUTES.map((r) => (
              <button
                key={r.key}
                onClick={() => onNavigate(r.key)}
                className={cn(
                  "whitespace-nowrap rounded-md px-2.5 py-1.5 text-sm",
                  route === r.key
                    ? "bg-primary font-medium text-primary-foreground"
                    : "text-muted-foreground",
                )}
              >
                {r.label}
              </button>
            ))}
          </nav>
        </header>

        <main className="mx-auto w-full max-w-[87.5rem] flex-1 p-4 md:p-6">
          {children}
        </main>
      </div>
    </div>
  );
}
