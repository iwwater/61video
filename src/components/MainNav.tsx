import { useState } from "react";
import { NavLink } from "react-router-dom";
import {
  BookOpen,
  Film,
  Images,
  Menu,
  Music4,
  Settings,
  Sparkles,
  Tv,
  Upload,
  X,
} from "lucide-react";

const navItems = [
  { to: "/shorts", label: "短视频", icon: Sparkles },
  { to: "/upload", label: "上传", icon: Upload },
  { to: "/list", label: "视频", icon: Film },
  { to: "/galleries", label: "图集", icon: Images },
  { to: "/novels", label: "小说", icon: BookOpen },
  { to: "/audio", label: "音频", icon: Music4 },
  { to: "/anime", label: "动漫", icon: Tv },
  { to: "/admin", label: "后台", icon: Settings },
];

export function MainNav() {
  const [open, setOpen] = useState(false);

  return (
    <nav className={`main-nav ${open ? "is-open" : ""}`}>
      <div className="container main-nav__inner">
        <NavLink to="/" className="main-nav__logo">
          <span className="main-nav__logo-mark">
            <picture>
              <source srcSet="/icon.webp" type="image/webp" />
              <img src="/icon.png" alt="" className="main-nav__logo-img" />
            </picture>
          </span>
        </NavLink>

        <ul className="main-nav__list" role="menubar">
          {navItems.map(({ to, label, icon: Icon }) => (
            <li key={to} role="none">
              <NavLink
                to={to}
                role="menuitem"
                className={({ isActive }) =>
                  `main-nav__link ${isActive ? "is-active" : ""}`
                }
                onClick={() => {
                  setOpen(false);
                  if (to === "/shorts") {
                    const el = document.documentElement;
                    // eslint-disable-next-line @typescript-eslint/no-explicit-any
                    const fn = el.requestFullscreen?.bind(el) || (el as any).webkitRequestFullscreen?.bind(el);
                    if (fn) {
                      try {
                        const ret = fn();
                        if (ret && typeof ret.then === "function") {
                          ret.catch(() => {});
                        }
                      } catch {
                        // ignore
                      }
                    }
                  }
                }}
              >
                <Icon size={16} />
                {label}
              </NavLink>
            </li>
          ))}
        </ul>

        <button
          className="main-nav__toggle"
          aria-label={open ? "关闭菜单" : "打开菜单"}
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? <X size={22} /> : <Menu size={22} />}
        </button>
      </div>
    </nav>
  );
}
