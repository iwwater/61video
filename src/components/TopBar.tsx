import { useEffect, useRef, useState, KeyboardEvent, FormEvent } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { Search } from "lucide-react";

/**
 * 顶部 TopBar。
 *
 * 当前只放一个全局搜索框 + 占位（主导航由 MainNav 负责，不要在这里堆元素）。
 * - 占位："搜索 视频 / 音频 / 图集 / 小说..."
 * - 回车：跳 /search?q=<query>
 * - "/"：从全局聚焦（受 keyboard-shortcuts 的 useShortcuts 调度，自身只暴露 focus 方法）
 *
 * 暴露：
 *   window.__topBarFocusSearch  : 全局可调的聚焦函数
 */
export function TopBar() {
  const navigate = useNavigate();
  const location = useLocation();
  const [q, setQ] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);

  // 挂到 window 上方便快捷键系统从任意位置触发
  useEffect(() => {
    const w = window as unknown as {
      __topBarFocusSearch?: (() => void) | undefined;
    };
    w.__topBarFocusSearch = () => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.select();
    };
    return () => {
      // 卸载时清掉引用，避免 dev 模式下挂载/卸载累积
      if (w.__topBarFocusSearch === undefined) return;
      try {
        delete w.__topBarFocusSearch;
      } catch {
        w.__topBarFocusSearch = undefined;
      }
    };
  }, []);

  // 切页后清空搜索框内容（避免遗留上个页面的关键词）
  useEffect(() => {
    setQ("");
  }, [location.pathname]);

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const kw = q.trim();
    if (!kw) return;
    navigate(`/search?q=${encodeURIComponent(kw)}`);
  }

  function handleKey(e: KeyboardEvent<HTMLInputElement>) {
    // 在输入框里按 Esc 清空并失焦（不抢全局 Escape 行为）
    if (e.key === "Escape") {
      const el = inputRef.current;
      if (!el) return;
      if (q) {
        e.stopPropagation();
        setQ("");
        el.setSelectionRange(0, 0);
      } else {
        el.blur();
      }
    }
  }

  return (
    <div className="top-bar">
      <div className="top-bar__inner container">
        <form
          className="top-bar__search"
          role="search"
          onSubmit={handleSubmit}
        >
          <Search size={16} className="top-bar__search-icon" aria-hidden="true" />
          <input
            ref={inputRef}
            type="search"
            className="top-bar__search-input"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={handleKey}
            placeholder="搜索 视频 / 音频 / 图集 / 小说..."
            aria-label="全局搜索"
            enterKeyHint="search"
            autoComplete="off"
            spellCheck={false}
          />
          <kbd className="top-bar__shortcut" aria-hidden="true">
            /
          </kbd>
        </form>
      </div>
    </div>
  );
}
