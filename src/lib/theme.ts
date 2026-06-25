// 主题系统：管理 <html data-theme> 属性 + localStorage 缓存。
//
// 流程：
//   1. index.html 内联脚本在挂载前先把 localStorage 里的值同步到 <html>，
//      避免首屏闪烁。
//   2. main.tsx 调 syncThemeFromServer()，异步 GET /api/settings/theme，
//      若与本地不同则覆盖。
//   3. 管理后台 ThemePage 切换时调 applyTheme(theme)，立刻生效。
//
// 公开端点 /api/settings/theme 不需要登录，原因见 backend/internal/api/api.go 中
// 的注释——登录页本身就要在用户登录之前正确显示主题。

// 后端真实支持的 3 套主题（决定全局下发到所有访客的值）
export type Theme = "dark" | "pink" | "sky";
export const THEMES: Theme[] = ["dark", "pink", "sky"];

// P1 主题：客户端本地切换用的 4 套主题（仅影响当前浏览器，不影响其他访客）
//   light    -> 落到 pink（奶油白）
//   dark     -> 落到 dark
//   eyecare  -> 落到 sky（蓝底对眼睛友好）
//   green    -> 落到 pink 后由 P1 主题 CSS 覆盖成绿色
export type P1Theme = "light" | "dark" | "eyecare" | "green";
export const P1_THEMES: P1Theme[] = ["light", "dark", "eyecare", "green"];

const STORAGE_KEY = "video-site:theme";

/**
 * 把 4 套 P1 主题映射到后端认识的 3 套 data-theme。
 * 客户端用 P1 主题做本地切换时，通过这个映射写到 DOM 上。
 * "green" 暂时映射到 pink（避免破坏后端契约），P1 主题 CSS 用 .theme-p1-green
 * 类来叠绿色覆盖层。
 */
const P1_TO_DATA: Record<P1Theme, Theme> = {
  light: "pink",
  dark: "dark",
  eyecare: "sky",
  green: "pink",
};

const DATA_TO_P1: Record<Theme, P1Theme> = {
  dark: "dark",
  pink: "light",
  sky: "eyecare",
};

function isTheme(value: unknown): value is Theme {
  return value === "dark" || value === "pink" || value === "sky";
}

function applyLocalClass(theme: P1Theme): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  // 旧的 P1 主题类先清掉
  root.classList.remove("theme-p1-light", "theme-p1-dark", "theme-p1-eyecare", "theme-p1-green");
  root.classList.add(`theme-p1-${theme}`);
}

/**
 * 拿到当前 DOM 上生效的主题（后端 3 套之一）。
 * 如果 <html data-theme> 没设，返回 "dark"（兜底）。
 */
export function getCurrentTheme(): Theme {
  if (typeof document === "undefined") return "dark";
  const v = document.documentElement.getAttribute("data-theme");
  return isTheme(v) ? v : "dark";
}

/**
 * 拿到当前 DOM 上生效的 P1 主题（4 选 1）。
 * 优先看 P1 主题类（class 模式），其次看 data-theme 并反推。
 */
export function getCurrentP1Theme(): P1Theme {
  if (typeof document === "undefined") return "dark";
  const root = document.documentElement;
  const classes = root.classList;
  for (const t of P1_THEMES) {
    if (classes.contains(`theme-p1-${t}`)) return t;
  }
  return DATA_TO_P1[getCurrentTheme()];
}

/**
 * 立即把后端主题应用到 <html data-theme> 并写入 localStorage。
 * 用于管理后台切换时本地立即生效。
 *
 * 入参非法时（旧版后端可能不返主题字段，此时 theme 会是 undefined / ""）
 * 直接忽略，避免 setAttribute("data-theme", "undefined") 这类污染。
 */
export function applyTheme(theme: Theme | string | undefined | null): void {
  if (!isTheme(theme)) {
    return;
  }
  if (typeof document !== "undefined") {
    document.documentElement.setAttribute("data-theme", theme);
    // 同步 P1 主题 class
    applyLocalClass(DATA_TO_P1[theme]);
  }
  try {
    localStorage.setItem(STORAGE_KEY, DATA_TO_P1[theme]);
  } catch {
    // 隐私模式 / quota 用尽：忽略
  }
}

/**
 * 应用 P1 主题（4 选 1）。只影响当前浏览器，不影响其他访客。
 * 落到对应的 data-theme + class。
 */
export function applyP1Theme(theme: P1Theme): void {
  if (typeof document !== "undefined") {
    const dataTheme = P1_TO_DATA[theme];
    document.documentElement.setAttribute("data-theme", dataTheme);
    applyLocalClass(theme);
  }
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // ignore
  }
}

/**
 * 从公开端点 /api/settings/theme 拉服务端配置的主题，覆盖本地。
 * 失败时不抛错，只是保持本地缓存的值。
 */
export async function syncThemeFromServer(): Promise<Theme> {
  try {
    const res = await fetch("/api/settings/theme", {
      credentials: "include",
      cache: "no-store",
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = (await res.json()) as { theme?: unknown };
    if (isTheme(data.theme)) {
      applyTheme(data.theme);
      return data.theme;
    }
  } catch {
    // 网络失败：保留 localStorage / data-theme 的现状
  }
  return getCurrentTheme();
}
