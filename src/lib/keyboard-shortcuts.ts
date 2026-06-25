import { useEffect } from "react";

/**
 * 全局键盘快捷键。
 *
 * 行为约定：
 * - 只在用户没有聚焦在 input / textarea / [contenteditable] 上时触发（避免抢表单输入）
 * - 任何按下的键如果带 modifier（Ctrl / Meta / Alt），全部忽略（留给浏览器/扩展）
 * - "/" 在任何位置都尝试聚焦 TopBar 搜索框（用 window.__topBarFocusSearch 这个挂载点）
 * - "Escape" 走原生行为（关闭 fullscreen / 关 modal），不强制处理
 * - "j" / "k" / "f" 通过 CustomEvent 派发到 window，列表页订阅后自行处理选择/全屏
 *
 * 用法（在 main.tsx 挂一次即可）：
 *   <ShortcutsProvider />
 *
 * 列表页订阅：
 *   useEffect(() => {
 *     const onJ = () => selectNext();
 *     window.addEventListener("shortcut:next", onJ);
 *     return () => window.removeEventListener("shortcut:next", onJ);
 *   }, [selectNext]);
 *
 * CustomEvent 名：
 *   "shortcut:next"      j  -> 下一个
 *   "shortcut:prev"      k  -> 上一个
 *   "shortcut:fullscreen" f  -> 全屏当前视频
 */

const SELECTOR_INPUT =
  'input:not([type="checkbox"]):not([type="radio"]):not([type="range"]):not([type="button"]):not([type="submit"]):not([type="reset"]):not([type="image"]):not([type="color"]):not([type="file"]), textarea, [contenteditable=""], [contenteditable="true"]';

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  // 优先看原生属性
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  // 用选择器兜底：把 type=button/submit 之类的排除
  try {
    if (target.matches(SELECTOR_INPUT)) return true;
  } catch {
    // matches 在某些旧元素上可能抛错，忽略
  }
  return false;
}

function dispatch(name: string): void {
  window.dispatchEvent(new CustomEvent(name));
}

function focusTopBarSearch(): void {
  const w = window as unknown as {
    __topBarFocusSearch?: (() => void) | undefined;
  };
  if (typeof w.__topBarFocusSearch === "function") {
    w.__topBarFocusSearch();
  }
}

function isEditableHost(target: EventTarget | null): boolean {
  if (!isTypingTarget(target)) return false;
  const el = target as HTMLElement;
  // 一些 input 在 disabled / readonly 时仍想触发快捷键，放行
  if (el instanceof HTMLInputElement) {
    if (el.disabled || el.readOnly) return false;
  }
  return true;
}

/**
 * 把一个 key 字符串 + 监听器塞进 map。重复 key 会被覆盖。
 */
export type ShortcutMap = Record<
  string,
  ((event: KeyboardEvent) => void) | undefined
>;

/**
 * 注册一组全局快捷键。组件卸载时自动清理。
 *
 * key 字符串支持空格分隔的修饰符："j" / "shift j" / "ctrl s" 等。
 * 这里只关心"非修饰键"，所有带 Ctrl/Meta/Alt 的事件直接忽略。
 */
export function useShortcuts(map: ShortcutMap): void {
  useEffect(() => {
    function handler(event: KeyboardEvent) {
      // 带 modifier 一律不抢，留给浏览器/扩展
      if (event.ctrlKey || event.metaKey || event.altKey) return;

      // 用户在输入控件里：不抢
      if (isEditableHost(event.target)) return;

      const key = event.key.toLowerCase();
      const fn = map[key];
      if (!fn) return;
      // 阻止默认行为（例如 "/" 在某些浏览器会触发 quick-find）
      event.preventDefault();
      fn(event);
    }

    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
    // map 是对象，调用方需自行 useCallback 或保证引用稳定
    // 这里故意不展开依赖数组，让调用方自己控制
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(Object.keys(map).sort())]);
}

/**
 * 全局快捷键 Provider。挂一次，挂在前几层即可（main.tsx）。
 * 提供：
 *   /       -> 聚焦 TopBar 搜索框
 *   j       -> 派发 "shortcut:next"（下一个视频/项目）
 *   k       -> 派发 "shortcut:prev"（上一个）
 *   f       -> 派发 "shortcut:fullscreen"（全屏当前视频）
 *   Escape  -> 关闭 fullscreen（浏览器原生行为，这里只是兜底）
 */
export function ShortcutsProvider() {
  useShortcuts({
    "/": focusTopBarSearch,
    j: () => dispatch("shortcut:next"),
    k: () => dispatch("shortcut:prev"),
    f: () => dispatch("shortcut:fullscreen"),
    escape: () => {
      // 仅在当前是 fullscreen 时尝试退出
      if (document.fullscreenElement) {
        document.exitFullscreen().catch(() => undefined);
      } else {
        // 仍然派发一个事件，让 modal/抽屉监听
        dispatch("shortcut:escape");
      }
    },
  });
  return null;
}
