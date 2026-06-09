import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, CircleStop, Download, Link as LinkIcon, Plus, Save, Trash2, Upload } from "lucide-react";
import * as api from "./api";
import { useToast } from "./ToastContext";
import { driveKindAbbr, generationStateClass, generationStateLabel } from "./drive/constants";
import { SpiderIcon } from "./icons/SpiderIcon";

type CrawlerForm = {
  id: string;
  name: string;
  builtin: string;
  scriptPath: string;
  pythonPath: string;
  targetNew: string;
  proxy: string;
  configJson: string;
};

const emptyForm: CrawlerForm = {
  id: "",
  name: "",
  builtin: "",
  scriptPath: "",
  pythonPath: "python3",
  targetNew: "10",
  proxy: "",
  configJson: "",
};

export function CrawlersPage() {
  const [list, setList] = useState<api.AdminCrawler[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [form, setForm] = useState<CrawlerForm>(emptyForm);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [runningId, setRunningId] = useState("");
  const [stoppingId, setStoppingId] = useState("");
  const [scriptURL, setScriptURL] = useState("");
  const [importingScript, setImportingScript] = useState(false);
  const [mode, setMode] = useState<"list" | "detail">("list");
  const { show } = useToast();

  const selected = useMemo(
    () => list.find((item) => item.id === selectedId) ?? null,
    [list, selectedId]
  );

  async function refresh() {
    setLoading(true);
    try {
      const data = await api.listCrawlers();
      setList(data);
    } catch (e) {
      show(e instanceof Error ? e.message : "加载爬虫失败", "error");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  function selectCrawler(crawler: api.AdminCrawler) {
    setSelectedId(crawler.id);
    setMode("detail");
    setForm({
      id: crawler.id,
      name: crawler.name,
      builtin: crawler.builtin ?? "",
      scriptPath: crawler.scriptPath ?? "",
      pythonPath: crawler.pythonPath || "python3",
      targetNew: crawler.targetNew || (crawler.builtin === "spider91" || crawler.kind === "spider91" ? "15" : "10"),
      proxy: crawler.proxy ?? "",
      configJson: crawler.configJson ?? "",
    });
  }

  function createCustom() {
    setSelectedId("");
    setForm(emptyForm);
    setScriptURL("");
    setMode("detail");
  }

  function createSpider91() {
    setSelectedId("");
    setForm({
      ...emptyForm,
      id: "spider91",
      name: "91 爬虫",
      builtin: "spider91",
      scriptPath: "",
      targetNew: "15",
    });
    setScriptURL("");
    setMode("detail");
  }

  function backToList() {
    setSelectedId("");
    setForm(emptyForm);
    setScriptURL("");
    setMode("list");
  }

  function set<K extends keyof CrawlerForm>(key: K, value: CrawlerForm[K]) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  async function save() {
    const id = form.id.trim();
    const name = form.name.trim();
    if (!id || !name) {
      show("请填写爬虫 ID 和名称", "error");
      return;
    }
    if (!form.builtin && !form.scriptPath.trim()) {
      show("请先导入爬虫脚本", "error");
      return;
    }
    setSaving(true);
    try {
      const resp = await api.upsertCrawler({
        id,
        name,
        builtin: form.builtin,
        scriptPath: form.scriptPath.trim(),
        pythonPath: form.pythonPath.trim(),
        targetNew: form.targetNew.trim(),
        proxy: form.proxy.trim(),
        configJson: form.configJson.trim(),
      });
      if (resp.warning) {
        show(`已保存，但初始化失败：${resp.warning}`, "error");
      } else {
        show("已保存", "success");
      }
      setSelectedId(id);
      await refresh();
      setMode("list");
    } catch (e) {
      show(e instanceof Error ? e.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  async function importScriptFile(file: File | null | undefined) {
    if (!file) return;
    setImportingScript(true);
    try {
      const resp = await api.importCrawlerScriptFile(file);
      set("scriptPath", resp.scriptPath);
      show("脚本已导入", "success");
    } catch (e) {
      show(e instanceof Error ? e.message : "导入失败", "error");
    } finally {
      setImportingScript(false);
    }
  }

  async function importScriptURL() {
    const url = scriptURL.trim();
    if (!url) {
      show("请填写脚本链接", "error");
      return;
    }
    setImportingScript(true);
    try {
      const resp = await api.importCrawlerScriptURL(url);
      set("scriptPath", resp.scriptPath);
      setScriptURL("");
      show("脚本已导入", "success");
    } catch (e) {
      show(e instanceof Error ? e.message : "导入失败", "error");
    } finally {
      setImportingScript(false);
    }
  }

  async function run(crawler: api.AdminCrawler) {
    setRunningId(crawler.id);
    try {
      const resp = await api.runCrawler(crawler.id);
      if (!resp.accepted) {
        show(resp.message || "当前爬虫有正在进行的任务", "info");
        return;
      }
      show("已触发抓取任务", "success");
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "触发失败", "error");
    } finally {
      setRunningId("");
    }
  }

  async function stop(crawler: api.AdminCrawler) {
    setStoppingId(crawler.id);
    try {
      const resp = await api.stopCrawlerTasks(crawler.id);
      show(resp.stopped ? "已请求停止任务" : "当前没有可停止任务", "info");
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "停止失败", "error");
    } finally {
      setStoppingId("");
    }
  }

  async function remove(crawler: api.AdminCrawler) {
    if (!window.confirm(`删除爬虫 ${crawler.name} 并清理它导入的视频？`)) return;
    try {
      const resp = await api.deleteCrawler(crawler.id);
      show(`已删除，并清理 ${resp.deletedVideos ?? 0} 个视频`, "success");
      setSelectedId("");
      setForm(emptyForm);
      setMode("list");
      await refresh();
    } catch (e) {
      show(e instanceof Error ? e.message : "删除失败", "error");
    }
  }

  return (
    <section className="admin-page">
      <header className="admin-page__header">
        <div>
          <h1 className="admin-page__title">爬虫管理</h1>
        </div>
        <div className="admin-detail-actions-inline">
          {mode === "list" ? (
            <button className="admin-btn is-primary" onClick={createCustom}>
              <Plus size={14} /> 添加爬虫
            </button>
          ) : (
            <button className="admin-btn" onClick={backToList}>
              <ArrowLeft size={14} /> 返回列表
            </button>
          )}
        </div>
      </header>

      {mode === "list" ? (
        <div className="admin-card admin-crawler-list">
          <header className="admin-card__title">
            <SpiderIcon size={16} /> 已配置爬虫
          </header>
          {loading ? (
            <div className="admin-loading">加载中...</div>
          ) : list.length === 0 ? (
            <div className="admin-empty">暂无爬虫</div>
          ) : (
            <div className="admin-drive-teasers">
              {list.map((crawler) => (
                <button
                  key={crawler.id}
                  type="button"
                  className={`admin-drive-teaser ${crawler.id === selectedId ? "is-active" : ""}`}
                  onClick={() => selectCrawler(crawler)}
                >
                  <span className="admin-drive-teaser__name">
                    <span className="admin-drive-card__brand-icon" data-kind={crawler.builtin || crawler.kind}>
                      {crawler.builtin === "spider91" ? "91" : driveKindAbbr(crawler.kind)}
                    </span>
                    {crawler.name}
                  </span>
                  <span className={`admin-status is-${crawler.status === "ok" ? "ok" : crawler.status === "error" ? "error" : "pending"}`}>
                    {crawler.status === "ok" ? "已就绪" : crawler.status === "error" ? "错误" : "未连接"}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="admin-crawler-detail">
          <div className="admin-card">
            <header className="admin-card__title">
              <SpiderIcon size={16} /> {selected ? "爬虫配置" : "添加爬虫"}
            </header>
            <div className="admin-form">
              {!selected && (
                <div className="admin-crawler-presets">
                  <button className={`admin-btn ${form.builtin === "" ? "is-primary" : ""}`} type="button" onClick={createCustom}>
                    <Plus size={13} /> 自定义脚本
                  </button>
                  <button className={`admin-btn ${form.builtin === "spider91" ? "is-primary" : ""}`} type="button" onClick={createSpider91}>
                    <SpiderIcon size={13} /> 内置 91
                  </button>
                </div>
              )}
              <div className="admin-form__row">
                <label htmlFor="crawler-id">爬虫 ID *</label>
                <input id="crawler-id" value={form.id} onChange={(e) => set("id", e.target.value)} disabled={!!selected} />
              </div>
              <div className="admin-form__row">
                <label htmlFor="crawler-name">名称 *</label>
                <input id="crawler-name" value={form.name} onChange={(e) => set("name", e.target.value)} />
              </div>
              {!form.builtin && (
                <div className="admin-form__row">
                  <label htmlFor="crawler-script-url">导入脚本</label>
                  <div className="admin-crawler-import">
                    <input
                      id="crawler-script-file"
                      className="admin-crawler-import__file"
                      type="file"
                      accept=".py,text/x-python"
                      disabled={importingScript}
                      onChange={(e) => {
                        importScriptFile(e.target.files?.[0]);
                        e.currentTarget.value = "";
                      }}
                    />
                    <label className="admin-btn" htmlFor="crawler-script-file" aria-disabled={importingScript}>
                      <Upload size={13} /> 上传文件
                    </label>
                    <input
                      id="crawler-script-url"
                      value={scriptURL}
                      onChange={(e) => setScriptURL(e.target.value)}
                      placeholder="https://example.com/crawler.py"
                      disabled={importingScript}
                    />
                    <button className="admin-btn" type="button" onClick={importScriptURL} disabled={importingScript}>
                      <LinkIcon size={13} /> {importingScript ? "导入中..." : "链接导入"}
                    </button>
                  </div>
                  {form.scriptPath && <div className="admin-form__help">脚本已导入</div>}
                </div>
              )}
              <div className="admin-form__row">
                <label htmlFor="crawler-target">每次补充新视频数</label>
                <input id="crawler-target" value={form.targetNew} onChange={(e) => set("targetNew", e.target.value)} placeholder="10" />
              </div>
              <div className="admin-form__row">
                <label htmlFor="crawler-proxy">代理地址</label>
                <input id="crawler-proxy" value={form.proxy} onChange={(e) => set("proxy", e.target.value)} placeholder="http://127.0.0.1:7890" />
              </div>
              <div className="admin-detail-actions">
                <button className="admin-btn is-primary" onClick={save} disabled={saving}>
                  <Save size={13} /> {saving ? "保存中..." : "保存"}
                </button>
                {selected && (
                  <>
                    <button className="admin-btn" onClick={() => run(selected)} disabled={runningId === selected.id}>
                      <Download size={13} /> {runningId === selected.id ? "触发中..." : "立即抓取"}
                    </button>
                    <button className="admin-btn is-stop" onClick={() => stop(selected)} disabled={stoppingId === selected.id}>
                      <CircleStop size={13} /> {stoppingId === selected.id ? "停止中..." : "停止任务"}
                    </button>
                    <button className="admin-btn is-danger" onClick={() => remove(selected)}>
                      <Trash2 size={13} /> 删除
                    </button>
                  </>
                )}
              </div>
            </div>
          </div>

          {selected && (
            <div className="admin-card admin-crawler-status">
              <header className="admin-card__title">
                <Download size={16} /> 状态
              </header>
              <div className="admin-gen-columns">
                <CrawlerStatus label="抓取" status={selected.scanGenerationStatus} />
                <CrawlerStatus label="封面" status={selected.thumbnailGenerationStatus} />
                <CrawlerStatus label="预览视频" status={selected.previewGenerationStatus} />
                <CrawlerStatus label="视频指纹" status={selected.fingerprintGenerationStatus} />
              </div>
              {selected.lastError && <div className="admin-detail-error">{selected.lastError}</div>}
            </div>
          )}
        </div>
      )}
    </section>
  );
}

function CrawlerStatus({ label, status }: { label: string; status?: api.DriveGenerationStatus }) {
  const state = status?.state || "idle";
  const labelText = label === "抓取" && state === "scanning" ? "抓取中" : generationStateLabel(state);
  return (
    <div className="admin-gen-col">
      <div className="admin-gen-col__head">
        <span className="admin-gen-col__label">{label}</span>
        <span className={`admin-status admin-generation-state is-${generationStateClass(state)}`}>
          {labelText}
        </span>
      </div>
      {label === "抓取" && (
        <div className="admin-gen-col__counts admin-gen-col__counts--scan">
          <div className="admin-gen-col__count"><span>已抓取</span><strong>{status?.scannedCount ?? 0}</strong></div>
          <div className="admin-gen-col__count"><span>预计新增</span><strong>{status?.addedCount ?? 0}</strong></div>
        </div>
      )}
    </div>
  );
}
