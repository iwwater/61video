import { useEffect, useState } from "react";
import {
  RefreshCw,
  Search,
  Trash2,
  BookOpen,
} from "lucide-react";
import * as api from "./api";
import { useToast } from "./ToastContext";
import { ConfirmModal } from "./ConfirmModal";

const PAGE_SIZE = 50;

export function NovelsAdminPage() {
  const [list, setList] = useState<api.AdminNovelSet[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [keyword, setKeyword] = useState("");
  const [searchKeyword, setSearchKeyword] = useState("");
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [deleteTarget, setDeleteTarget] = useState<api.AdminNovelSet | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteSource, setDeleteSource] = useState(false);
  const { show } = useToast();

  async function refresh() {
    setLoading(true);
    setLoadError("");
    try {
      const r = await api.listAdminNovels({ page, size: PAGE_SIZE, keyword: searchKeyword });
      setList(r.items ?? []);
      setTotal(r.total ?? 0);
    } catch (e) {
      const message = e instanceof Error ? e.message : "加载失败";
      setLoadError(message);
      show(message, "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
  }, [page, searchKeyword]);

  useEffect(() => {
    if (keyword === searchKeyword) return;
    const timer = window.setTimeout(() => {
      setSearchKeyword(keyword);
      setPage(1);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [keyword]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const pageStart = total === 0 ? 0 : (page - 1) * PAGE_SIZE + 1;
  const pageEnd = Math.min(total, page * PAGE_SIZE);
  const listSummary = `共 ${total} 本小说/文档，第 ${page} / ${totalPages} 页，显示 ${pageStart}-${pageEnd}`;

  async function confirmDelete() {
    if (!deleteTarget) return;
    const target = deleteTarget;
    setDeleting(true);
    try {
      const result = await api.deleteNovelSet(target.id, { deleteSource });
      setDeleteTarget(null);
      setDeleteSource(false);
      show(result.deletedSource ? "已删除小说，并清理源文件" : "已删除小说", "success");
      if (list.length === 1 && page > 1) {
        setPage((p) => Math.max(1, p - 1));
      } else {
        refresh();
      }
    } catch (e) {
      show(e instanceof Error ? e.message : "删除失败", "error");
    } finally {
      setDeleting(false);
    }
  }

  function handleSearchSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSearchKeyword(keyword);
    setPage(1);
  }

  function contentTypeLabel(ct: string): string {
    if (ct === "pdf") return "PDF";
    if (ct === "text") return "文本";
    return ct || "—";
  }

  return (
    <section>
      <header className="admin-page__header">
        <h1 className="admin-page__title">小说管理</h1>
      </header>

      <div className="admin-page__actions admin-videos-filter">
        <form className="admin-videos-filter__search" onSubmit={handleSearchSubmit}>
          <Search size={14} className="admin-videos-filter__search-icon" />
          <input
            aria-label="搜索小说标题"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索小说标题"
          />
        </form>
        <button type="button" className="admin-btn" onClick={refresh}>
          <RefreshCw size={13} /> 刷新
        </button>
      </div>

      {!loading && (
        <div className="admin-videos-list-toolbar">
          <div className="admin-videos-summary">{listSummary}</div>
        </div>
      )}

      {loading ? (
        <div className="admin-loading-state">
          <RefreshCw size={20} className="admin-spin" />
          <span>加载中...</span>
        </div>
      ) : loadError ? (
        <div className="admin-error-state">
          <strong>加载失败</strong>
          <span>{loadError}</span>
          <button type="button" className="admin-btn" onClick={refresh}>
            <RefreshCw size={13} /> 重试
          </button>
        </div>
      ) : list.length === 0 ? (
        <div className="admin-empty-state">
          <div className="admin-empty-state__icon">
            <BookOpen size={48} />
          </div>
          <div className="admin-empty-state__text">
            {searchKeyword
              ? "未匹配到搜索结果。"
              : "还没有小说/文档。运行一次扫描后，PDF/EPUB/TXT 文件会被自动归为小说条目。"}
          </div>
        </div>
      ) : (
        <>
          <table className="admin-table admin-videos-table">
            <thead>
              <tr>
                <th>标题</th>
                <th>作者</th>
                <th>类型</th>
                <th>章节数</th>
                <th>来源</th>
                <th className="is-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {list.map((item) => (
                <tr key={item.id}>
                  <td data-label="标题">{item.title}</td>
                  <td data-label="作者">{item.author || <span className="admin-text-faint">—</span>}</td>
                  <td data-label="类型">{contentTypeLabel(item.contentType)}</td>
                  <td data-label="章节数">{item.chapterCount}</td>
                  <td data-label="来源" className="admin-mono-cell">
                    {item.sourceKind === "scanner" ? "扫描" : item.sourceKind}
                  </td>
                  <td className="is-actions" data-label="操作">
                    <button
                      type="button"
                      className="admin-btn is-danger"
                      onClick={() => {
                        setDeleteSource(false);
                        setDeleteTarget(item);
                      }}
                      title="删除小说"
                    >
                      <Trash2 size={13} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="admin-table-pagination">
            <button type="button" className="admin-btn" onClick={() => setPage(1)} disabled={page <= 1}>
              首页
            </button>
            <button type="button" className="admin-btn" onClick={() => setPage((p) => Math.max(1, p - 1))} disabled={page <= 1}>
              上一页
            </button>
            <span className="admin-table-pagination__info">
              第 {page} / {totalPages} 页，每页 {PAGE_SIZE} 个
            </span>
            <button
              type="button"
              className="admin-btn"
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
            >
              下一页
            </button>
            <button type="button" className="admin-btn" onClick={() => setPage(totalPages)} disabled={page >= totalPages}>
              末页
            </button>
          </div>
        </>
      )}

      <ConfirmModal
        open={deleteTarget !== null}
        title="删除小说"
        message={deleteTarget ? `确定要删除「${deleteTarget.title}」吗？` : ""}
        confirmText="删除小说"
        danger
        centerMessage
        modalClassName="admin-modal--delete-confirm"
        loading={deleting}
        onCancel={() => {
          if (!deleting) {
            setDeleteTarget(null);
            setDeleteSource(false);
          }
        }}
        onConfirm={confirmDelete}
      >
        <label className="admin-delete-source-option">
          <input
            type="checkbox"
            checked={deleteSource}
            disabled={deleting}
            onChange={(e) => setDeleteSource(e.target.checked)}
          />
          <span>
            <strong>同时删除网盘中的源文件</strong>
            <small>开启后会先删除源文件，失败则不会删除管理库记录。</small>
          </span>
        </label>
      </ConfirmModal>
    </section>
  );
}
