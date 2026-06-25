import { useEffect, useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import {
  HardDrive,
  Film,
  LogOut,
  Home,
  Tags,
  Palette,
  RefreshCw,
  MoreVertical,
  Tv,
  Database,
  Images,
  BookOpen,
} from "lucide-react";
import * as api from "./api";
import { useAuth } from "./AuthContext";
import { useToast } from "./ToastContext";
import { SpiderIcon } from "./icons/SpiderIcon";
// admin.css 只在进入 /admin/* 路由后才需要；放这里动态 import，
// 避免普通访客首屏多拉一份 admin 样式。
import "../styles/admin.css";

// admin.css 只在后台路由被打开时按需加载，首页/视频详情页等不需要。
// Vite 会把它打成单独的 chunk，首屏不再 bundle 118KB admin 样式。
import("../styles/admin.css");

export function AdminLayout() {
  const { logout } = useAuth();
  const navigate = useNavigate();
  const { show } = useToast();
  const [checkingUpdate, setCheckingUpdate] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  useEffect(() => {
    if (!mobileMenuOpen) return;
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === "Escape") {
        setMobileMenuOpen(false);
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [mobileMenuOpen]);

  async function handleCheckUpdate() {
    if (checkingUpdate) return;
    setCheckingUpdate(true);
    try {
      const result = await api.checkUpdate();
      if (result.hasUpdate) {
        show(
          `发现新版本 ${result.latestVersion}，当前 ${result.currentVersion}`,
          "success"
        );
        return;
      }
      if (result.currentVersion === "unknown") {
        show(`当前版本未知，GitHub 最新版本为 ${result.latestVersion}`, "info");
        return;
      }
      show(`当前已是最新版本：${result.currentVersion}`, "success");
    } catch {
      show("检查更新失败，请稍后重试", "error");
    } finally {
      setCheckingUpdate(false);
    }
  }

  async function handleLogout() {
    try {
      await logout();
      show("已退出登录", "success");
      navigate("/login", { replace: true });
    } catch {
      show("退出失败", "error");
    }
  }

  return (
    <div className="admin-shell">
      <aside className="admin-sidebar">
        <nav className="admin-nav">
          <div className="admin-nav__group admin-nav__group--home">
            <span className="admin-nav__group-label">主站</span>
            <NavLink to="/" className="admin-nav__link">
              <span className="admin-nav__icon"><Home size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">返回主站</span>
              </span>
            </NavLink>
          </div>
          <div className="admin-nav__group">
            <span className="admin-nav__group-label">资源</span>
            <NavLink
              to="/admin/drives"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><HardDrive size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">网盘管理</span>
              </span>
            </NavLink>
            <NavLink
              to="/admin/crawlers"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><SpiderIcon size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">爬虫管理</span>
              </span>
            </NavLink>
          </div>
          <div className="admin-nav__group">
            <span className="admin-nav__group-label">管理</span>
            <NavLink
              to="/admin/videos"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><Film size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">视频管理</span>
              </span>
            </NavLink>
            <NavLink
              to="/admin/tags"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><Tags size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">标签管理</span>
              </span>
            </NavLink>
            <NavLink
              to="/admin/image-sets"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><Images size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">图集管理</span>
              </span>
            </NavLink>
            <NavLink
              to="/admin/novels"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><BookOpen size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">小说管理</span>
              </span>
            </NavLink>
          </div>
          <div className="admin-nav__group">
            <span className="admin-nav__group-label">系统</span>
            <NavLink
              to="/admin/parse-sources"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><Tv size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">解析源</span>
              </span>
            </NavLink>
            <NavLink
              to="/admin/resource-sites"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><Database size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">资源站</span>
              </span>
            </NavLink>
            <NavLink
              to="/admin/theme"
              className={({ isActive }) =>
                `admin-nav__link ${isActive ? "is-active" : ""}`
              }
            >
              <span className="admin-nav__icon"><Palette size={16} /></span>
              <span className="admin-nav__text">
                <span className="admin-nav__title">主题外观</span>
              </span>
            </NavLink>
          </div>
        </nav>
        <div className="admin-sidebar__footer">
          <button
            className="admin-sidebar__check-update"
            onClick={handleCheckUpdate}
            disabled={checkingUpdate}
          >
            <RefreshCw size={14} />
            {checkingUpdate ? "检查中" : "检查更新"}
          </button>
          <button className="admin-sidebar__logout" onClick={handleLogout}>
            <LogOut size={14} />
            退出登录
          </button>
        </div>
        <button
          className="admin-sidebar__mobile-menu"
          onClick={() => setMobileMenuOpen((v) => !v)}
          aria-label="更多操作"
        >
          <MoreVertical size={18} />
        </button>
      </aside>
      {mobileMenuOpen && (
        <div className="admin-sidebar__mobile-overlay" onClick={() => setMobileMenuOpen(false)} />
      )}
      <div className={`admin-sidebar__mobile-panel${mobileMenuOpen ? " is-open" : ""}`}>
        <NavLink to="/" className="admin-sidebar__home" onClick={() => setMobileMenuOpen(false)}>
          <Home size={14} /> 返回主站
        </NavLink>
        <button
          className="admin-sidebar__check-update"
          onClick={() => { handleCheckUpdate(); setMobileMenuOpen(false); }}
          disabled={checkingUpdate}
        >
          <RefreshCw size={14} />
          {checkingUpdate ? "检查中" : "检查更新"}
        </button>
        <button className="admin-sidebar__logout" onClick={() => { handleLogout(); setMobileMenuOpen(false); }}>
          <LogOut size={14} />
          退出登录
        </button>
      </div>
      <main className="admin-main">
        <Outlet />
      </main>
    </div>
  );
}
