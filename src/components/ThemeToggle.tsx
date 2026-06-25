import { useEffect, useState } from "react";
import { Eye, Leaf, Moon, Sun } from "lucide-react";
import {
  applyP1Theme,
  getCurrentP1Theme,
  type P1Theme,
} from "@/lib/theme";

const OPTIONS: Array<{
  id: P1Theme;
  label: string;
  title: string;
  Icon: typeof Sun;
}> = [
  { id: "light", label: "浅色", title: "浅色主题（cream）", Icon: Sun },
  { id: "dark", label: "深色", title: "深色主题", Icon: Moon },
  { id: "eyecare", label: "护眼", title: "护眼主题（蓝）", Icon: Eye },
  { id: "green", label: "绿色", title: "绿色主题", Icon: Leaf },
];

/**
 * 4 主题切换器。挂在 TopBar 右侧，点击按钮直接在 light/dark/eyecare/green 之间循环。
 *
 * 选中的主题写到 <html data-theme> + <html class> + localStorage。
 * 注意：4 套 P1 主题都是客户端本地覆盖，不下发到其他访客。
 */
export function ThemeToggle() {
  const [current, setCurrent] = useState<P1Theme>(getCurrentP1Theme());

  // 跨标签页同步：localStorage 变化时重读一次
  useEffect(() => {
    function onStorage(e: StorageEvent) {
      if (e.key !== "video-site:theme") return;
      setCurrent(getCurrentP1Theme());
    }
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  function handleSelect(next: P1Theme) {
    if (next === current) return;
    applyP1Theme(next);
    setCurrent(next);
  }

  return (
    <div
      className="theme-toggle"
      role="radiogroup"
      aria-label="主题切换"
    >
      {OPTIONS.map(({ id, label, title, Icon }) => {
        const active = id === current;
        return (
          <button
            key={id}
            type="button"
            className={`theme-toggle__btn${active ? " is-active" : ""}`}
            onClick={() => handleSelect(id)}
            title={title}
            aria-label={title}
            aria-pressed={active}
            role="radio"
            aria-checked={active}
          >
            <Icon size={15} aria-hidden="true" />
            <span className="theme-toggle__label">{label}</span>
          </button>
        );
      })}
    </div>
  );
}

export default ThemeToggle;
