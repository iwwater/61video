import { ChangeEvent, FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  BookOpen,
  Check,
  Film,
  Music4,
  Plus,
  Trash2,
  UploadCloud,
  X,
} from "lucide-react";
import { AppShell } from "@/components/AppShell";
import { SectionHeader } from "@/components/SectionHeader";
import { uploadVideo } from "@/data/videos";
import { createNovel, type CreateNovelChapterInput } from "@/data/novels";
import { defaultUploadTitleFromFileName } from "@/lib/uploadTitle";
import type { VideoItem } from "@/types";

const VIDEO_TAGS = ["人妻", "自慰", "口交", "熟女", "素人", "AV"];
const AUDIO_TAGS = ["ASMR", "音乐", "朗读", "环境音", "白噪音", "播客"];

type Tab = "video" | "audio" | "novel";

export default function UploadPage() {
  const [tab, setTab] = useState<Tab>("video");
  useEffect(() => {
    document.title = "上传 · 61";
  }, []);

  return (
    <AppShell>
      <div className="container page-section">
        <SectionHeader title="上传中心" extra="视频 / 音频 / 小说" />
        <div className="upload-tabs" role="tablist">
          <button
            role="tab"
            aria-selected={tab === "video"}
            className={`upload-tab ${tab === "video" ? "is-active" : ""}`}
            onClick={() => setTab("video")}
          >
            <Film size={16} /> 视频
          </button>
          <button
            role="tab"
            aria-selected={tab === "audio"}
            className={`upload-tab ${tab === "audio" ? "is-active" : ""}`}
            onClick={() => setTab("audio")}
          >
            <Music4 size={16} /> 音频
          </button>
          <button
            role="tab"
            aria-selected={tab === "novel"}
            className={`upload-tab ${tab === "novel" ? "is-active" : ""}`}
            onClick={() => setTab("novel")}
          >
            <BookOpen size={16} /> 小说
          </button>
        </div>

        {tab === "video" ? <VideoUploadForm /> : null}
        {tab === "audio" ? <AudioUploadForm /> : null}
        {tab === "novel" ? <NovelUploadForm /> : null}
      </div>
    </AppShell>
  );
}

// ---------- 视频上传 ----------

function VideoUploadForm() {
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [uploaded, setUploaded] = useState<VideoItem | null>(null);

  const fileMeta = useMemo(() => {
    if (!file) return "";
    const mb = file.size / 1024 / 1024;
    return `${file.name} · ${mb >= 1 ? mb.toFixed(1) : mb.toFixed(2)} MB`;
  }, [file]);

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const next = event.target.files?.[0] ?? null;
    setFile(next);
    setTitle(next ? defaultUploadTitleFromFileName(next.name) : "");
    setUploaded(null);
    setError("");
  }

  function toggleTag(tag: string) {
    setTags((cur) =>
      cur.includes(tag) ? cur.filter((t) => t !== tag) : [...cur, tag]
    );
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file || saving) return;
    setSaving(true);
    setError("");
    try {
      const created = await uploadVideo({ file, title, tags });
      setUploaded(created);
    } catch (e) {
      setError(e instanceof Error ? e.message : "上传失败");
    } finally {
      setSaving(false);
    }
  }

  if (uploaded) {
    return (
      <div className="upload-success">
        <Check size={32} />
        <h3>上传成功</h3>
        <p>「{uploaded.title}」已加入本地库。等待后台异步生成预览/封面。</p>
        <div className="upload-success__actions">
          <Link to={`/video/${uploaded.id}`} className="button button--primary">
            <Film size={16} /> 打开视频
          </Link>
          <button
            type="button"
            className="button button--ghost"
            onClick={() => {
              setUploaded(null);
              setFile(null);
              setTitle("");
              setTags([]);
            }}
          >
            <Plus size={16} /> 再传一个
          </button>
        </div>
      </div>
    );
  }

  return (
    <form className="upload-form" onSubmit={handleSubmit}>
      <FilePicker file={file} fileMeta={fileMeta} onChange={handleFileChange} />
      <label className="upload-field">
        标题
        <input
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="默认从文件名推断"
        />
      </label>
      <TagPicker tags={VIDEO_TAGS} value={tags} onToggle={toggleTag} />
      {error ? <p className="upload-error">{error}</p> : null}
      <button className="button button--primary" type="submit" disabled={!file || saving}>
        <UploadCloud size={16} />
        {saving ? "上传中..." : "上传视频"}
      </button>
    </form>
  );
}

// ---------- 音频上传 ----------

const AUDIO_EXT = [".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus"];

function AudioUploadForm() {
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [uploaded, setUploaded] = useState<VideoItem | null>(null);

  const fileMeta = useMemo(() => {
    if (!file) return "";
    const mb = file.size / 1024 / 1024;
    return `${file.name} · ${mb >= 1 ? mb.toFixed(1) : mb.toFixed(2)} MB`;
  }, [file]);

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const next = event.target.files?.[0] ?? null;
    if (next) {
      const ext = next.name.toLowerCase().slice(next.name.lastIndexOf("."));
      if (!AUDIO_EXT.includes(ext)) {
        setError(`不支持的音频格式：${ext}（支持 ${AUDIO_EXT.join(" ")})`);
        return;
      }
    }
    setError("");
    setFile(next);
    setTitle(next ? defaultUploadTitleFromFileName(next.name) : "");
    setUploaded(null);
  }

  function toggleTag(tag: string) {
    setTags((cur) =>
      cur.includes(tag) ? cur.filter((t) => t !== tag) : [...cur, tag]
    );
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!file || saving) return;
    setSaving(true);
    setError("");
    try {
      // 复用 /api/upload，后端按扩展名自动判 mediaType=audio
      const created = await uploadVideo({ file, title, tags });
      setUploaded(created);
    } catch (e) {
      setError(e instanceof Error ? e.message : "上传失败");
    } finally {
      setSaving(false);
    }
  }

  if (uploaded) {
    return (
      <div className="upload-success">
        <Check size={32} />
        <h3>上传成功</h3>
        <p>「{uploaded.title}」已加入音频库。预览视频生成已被自动禁用。</p>
        <div className="upload-success__actions">
          <Link to={`/audio`} className="button button--primary">
            <Music4 size={16} /> 打开音频列表
          </Link>
          <button
            type="button"
            className="button button--ghost"
            onClick={() => {
              setUploaded(null);
              setFile(null);
              setTitle("");
              setTags([]);
            }}
          >
            <Plus size={16} /> 再传一个
          </button>
        </div>
      </div>
    );
  }

  return (
    <form className="upload-form" onSubmit={handleSubmit}>
      <FilePicker file={file} fileMeta={fileMeta} onChange={handleFileChange} accept={AUDIO_EXT.join(",")} />
      <label className="upload-field">
        标题
        <input
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="默认从文件名推断"
        />
      </label>
      <TagPicker tags={AUDIO_TAGS} value={tags} onToggle={toggleTag} />
      {error ? <p className="upload-error">{error}</p> : null}
      <button className="button button--primary" type="submit" disabled={!file || saving}>
        <UploadCloud size={16} />
        {saving ? "上传中..." : "上传音频"}
      </button>
    </form>
  );
}

// ---------- 小说上传 ----------

type ChapterDraft = {
  title: string;
  body: string;
};

function NovelUploadForm() {
  const [id, setId] = useState("");
  const [title, setTitle] = useState("");
  const [author, setAuthor] = useState("");
  const [contentType, setContentType] = useState<"text" | "pdf">("text");
  const [tagsInput, setTagsInput] = useState("");
  const [description, setDescription] = useState("");
  const [chapters, setChapters] = useState<ChapterDraft[]>([
    { title: "第一章", body: "" },
  ]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [savedId, setSavedId] = useState<string | null>(null);
  const navigate = useNavigate();

  function addChapter() {
    setChapters((cur) => [
      ...cur,
      { title: `第 ${toChineseNum(cur.length + 1)} 章`, body: "" },
    ]);
  }

  function removeChapter(i: number) {
    setChapters((cur) => cur.filter((_, idx) => idx !== i));
  }

  function updateChapter(i: number, patch: Partial<ChapterDraft>) {
    setChapters((cur) =>
      cur.map((c, idx) => (idx === i ? { ...c, ...patch } : c))
    );
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!id.trim() || !title.trim()) {
      setError("ID 和书名必填");
      return;
    }
    if (chapters.length === 0) {
      setError("至少需要一个章节");
      return;
    }
    if (contentType === "text" && chapters.every((c) => !c.body.trim())) {
      setError("文本小说至少需要一个章节有正文");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const tags = tagsInput
        .split(/[,，]/)
        .map((s) => s.trim())
        .filter(Boolean);
      const chaptersInput: CreateNovelChapterInput[] = chapters.map((c, i) => ({
        position: i,
        title: c.title.trim() || `第 ${toChineseNum(i + 1)} 章`,
        contentType,
        body: c.body,
      }));
      const created = await createNovel({
        id: id.trim(),
        title: title.trim(),
        author: author.trim() || "未知",
        contentType,
        tags,
        description,
        chapters: chaptersInput,
      });
      setSavedId(created.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  if (savedId) {
    return (
      <div className="upload-success">
        <Check size={32} />
        <h3>已保存小说</h3>
        <p>「{title}」（{chapters.length} 章）已入库。</p>
        <div className="upload-success__actions">
          <Link to={`/novel/${savedId}`} className="button button--primary">
            <BookOpen size={16} /> 打开阅读
          </Link>
          <button
            type="button"
            className="button button--ghost"
            onClick={() => {
              setSavedId(null);
              setId("");
              setTitle("");
              setAuthor("");
              setDescription("");
              setChapters([{ title: "第一章", body: "" }]);
            }}
          >
            <Plus size={16} /> 再加一本
          </button>
        </div>
      </div>
    );
  }

  return (
    <form className="upload-form upload-form--novel" onSubmit={handleSubmit}>
      <div className="upload-form__row">
        <label className="upload-field upload-field--half">
          ID（唯一标识）
          <input
            type="text"
            value={id}
            onChange={(e) => setId(e.target.value)}
            placeholder="例如：my-novel-001"
            required
          />
        </label>
        <label className="upload-field upload-field--half">
          书名
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="例如：星辰大海"
            required
          />
        </label>
      </div>
      <div className="upload-form__row">
        <label className="upload-field upload-field--half">
          作者
          <input
            type="text"
            value={author}
            onChange={(e) => setAuthor(e.target.value)}
            placeholder="留空显示为「未知」"
          />
        </label>
        <label className="upload-field upload-field--half">
          类型
          <select
            value={contentType}
            onChange={(e) =>
              setContentType(e.target.value as "text" | "pdf")
            }
          >
            <option value="text">文本（HTML 正文）</option>
            <option value="pdf">PDF（每章填 PDF URL）</option>
          </select>
        </label>
      </div>
      <label className="upload-field">
        标签（逗号分隔）
        <input
          type="text"
          value={tagsInput}
          onChange={(e) => setTagsInput(e.target.value)}
          placeholder="玄幻, 穿越, 都市"
        />
      </label>
      <label className="upload-field">
        简介
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          placeholder="一段话介绍这本书"
        />
      </label>

      <div className="upload-form__chapters">
        <div className="upload-form__chapters-head">
          <h4>章节（{chapters.length}）</h4>
          <button
            type="button"
            className="button button--ghost button--sm"
            onClick={addChapter}
          >
            <Plus size={14} /> 添加章节
          </button>
        </div>
        {chapters.map((c, i) => (
          <div key={i} className="upload-chapter">
            <div className="upload-chapter__head">
              <span className="upload-chapter__num">#{i + 1}</span>
              <input
                type="text"
                value={c.title}
                onChange={(e) =>
                  updateChapter(i, { title: e.target.value })
                }
                placeholder="章节标题"
                className="upload-chapter__title"
              />
              {chapters.length > 1 ? (
                <button
                  type="button"
                  className="button button--ghost button--sm"
                  onClick={() => removeChapter(i)}
                  aria-label="删除章节"
                >
                  <Trash2 size={14} />
                </button>
              ) : null}
            </div>
            {contentType === "text" ? (
              <textarea
                value={c.body}
                onChange={(e) => updateChapter(i, { body: e.target.value })}
                rows={6}
                placeholder="<p>支持 HTML 标签，如 &lt;strong&gt;&lt;em&gt;&lt;img src=&quot;...&quot;&gt;</p>"
                className="upload-chapter__body"
              />
            ) : (
              <input
                type="url"
                value={c.body}
                onChange={(e) => updateChapter(i, { body: e.target.value })}
                placeholder="PDF 文件 URL"
                className="upload-chapter__body"
              />
            )}
          </div>
        ))}
      </div>

      {error ? <p className="upload-error">{error}</p> : null}

      <div className="upload-form__actions">
        <button
          type="button"
          className="button button--ghost"
          onClick={() => navigate("/novels")}
        >
          <X size={14} /> 取消
        </button>
        <button
          type="submit"
          className="button button--primary"
          disabled={saving}
        >
          <UploadCloud size={14} />
          {saving ? "保存中..." : "保存小说"}
        </button>
      </div>
    </form>
  );
}

// ---------- 通用 ----------

function FilePicker({
  file,
  fileMeta,
  onChange,
  accept,
}: {
  file: File | null;
  fileMeta: string;
  onChange: (e: ChangeEvent<HTMLInputElement>) => void;
  accept?: string;
}) {
  return (
    <label className="upload-drop">
      <input
        type="file"
        onChange={onChange}
        accept={accept}
        className="upload-drop__input"
      />
      <UploadCloud size={32} />
      <span className="upload-drop__hint">
        {file ? fileMeta : "点击或拖拽文件到这里（≤32 MiB）"}
      </span>
    </label>
  );
}

function TagPicker({
  tags,
  value,
  onToggle,
}: {
  tags: string[];
  value: string[];
  onToggle: (t: string) => void;
}) {
  return (
    <div className="upload-tags">
      <span className="upload-tags__label">标签：</span>
      {tags.map((t) => (
        <button
          key={t}
          type="button"
          className={`upload-tag ${value.includes(t) ? "is-active" : ""}`}
          onClick={() => onToggle(t)}
        >
          {t}
        </button>
      ))}
    </div>
  );
}

function toChineseNum(n: number): string {
  const map = ["零", "一", "二", "三", "四", "五", "六", "七", "八", "九"];
  if (n < 10) return map[n];
  if (n < 20) return `十${n % 10 === 0 ? "" : map[n % 10]}`;
  if (n < 100) {
    return `${map[Math.floor(n / 10)]}十${n % 10 === 0 ? "" : map[n % 10]}`;
  }
  return String(n);
}
