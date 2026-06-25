import { ReactElement, useCallback, useState } from "react";
import { Document, Page, pdfjs } from "react-pdf";
import "react-pdf/dist/Page/AnnotationLayer.css";
import "react-pdf/dist/Page/TextLayer.css";
import { ChevronLeft, ChevronRight, RotateCcw, ZoomIn, ZoomOut } from "lucide-react";

// Vite + ESM 友好的 worker 配置：让 Vite 把 worker 当作 asset 打包。
// 必须在使用 Document/Page 之前调用一次（顶层 module 副作用）。
pdfjs.GlobalWorkerOptions.workerSrc = new URL(
  "pdfjs-dist/build/pdf.worker.min.mjs",
  import.meta.url,
).toString();

type Props = {
  /**
   * PDF 文件地址（HTTP URL / Blob / data URL / Uint8Array 都可）。
   */
  file: string;
  /**
   * 阅读器顶部显示的标题（用于 a11y）。
   */
  title?: string;
  /**
   * 初始缩放比例，默认 1（100%）。范围 0.5 - 3。
   */
  initialScale?: number;
};

const SCALE_MIN = 0.5;
const SCALE_MAX = 3;
const SCALE_STEP = 0.25;

export function PdfViewer({ file, title, initialScale = 1 }: Props): ReactElement {
  const [numPages, setNumPages] = useState<number | null>(null);
  const [pageNumber, setPageNumber] = useState(1);
  const [scale, setScale] = useState<number>(() =>
    clamp(initialScale, SCALE_MIN, SCALE_MAX)
  );
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const onLoadSuccess = useCallback(
    ({ numPages: total }: { numPages: number }): void => {
      setNumPages(total);
      setPageNumber(1);
      setError(null);
      setLoading(false);
    },
    []
  );

  const onLoadError = useCallback((err: Error): void => {
    setError(err?.message || "PDF 加载失败");
    setLoading(false);
  }, []);

  const goPrev = useCallback((): void => {
    setPageNumber((p) => Math.max(1, p - 1));
  }, []);

  const goNext = useCallback((): void => {
    setPageNumber((p) => (numPages ? Math.min(numPages, p + 1) : p));
  }, [numPages]);

  const zoomIn = useCallback((): void => {
    setScale((s) => clamp(round2(s + SCALE_STEP), SCALE_MIN, SCALE_MAX));
  }, []);

  const zoomOut = useCallback((): void => {
    setScale((s) => clamp(round2(s - SCALE_STEP), SCALE_MIN, SCALE_MAX));
  }, []);

  const reset = useCallback((): void => {
    setScale(1);
    setPageNumber(1);
  }, []);

  const goToPage = useCallback((target: number): void => {
    if (!numPages) return;
    setPageNumber(clamp(Math.floor(target), 1, numPages));
  }, [numPages]);

  return (
    <div className="pdf-viewer" role="region" aria-label={title || "PDF 阅读器"}>
      <div className="pdf-viewer__toolbar" role="toolbar" aria-label="PDF 工具栏">
        <button
          type="button"
          className="pdf-viewer__btn"
          onClick={goPrev}
          disabled={pageNumber <= 1}
          aria-label="上一页"
        >
          <ChevronLeft size={16} />
        </button>
        <span className="pdf-viewer__page-info">
          <input
            type="number"
            className="pdf-viewer__page-input"
            value={pageNumber}
            min={1}
            max={numPages ?? undefined}
            onChange={(e) => goToPage(Number(e.target.value) || 1)}
            aria-label="跳转到页码"
          />
          <span className="pdf-viewer__page-sep">/</span>
          <span className="pdf-viewer__page-total">{numPages ?? "--"}</span>
        </span>
        <button
          type="button"
          className="pdf-viewer__btn"
          onClick={goNext}
          disabled={!numPages || pageNumber >= numPages}
          aria-label="下一页"
        >
          <ChevronRight size={16} />
        </button>

        <span className="pdf-viewer__sep" />

        <button
          type="button"
          className="pdf-viewer__btn"
          onClick={zoomOut}
          disabled={scale <= SCALE_MIN}
          aria-label="缩小"
        >
          <ZoomOut size={16} />
        </button>
        <span className="pdf-viewer__scale" aria-live="polite">
          {Math.round(scale * 100)}%
        </span>
        <button
          type="button"
          className="pdf-viewer__btn"
          onClick={zoomIn}
          disabled={scale >= SCALE_MAX}
          aria-label="放大"
        >
          <ZoomIn size={16} />
        </button>
        <button
          type="button"
          className="pdf-viewer__btn"
          onClick={reset}
          aria-label="重置"
          title="重置到第 1 页 100%"
        >
          <RotateCcw size={16} />
        </button>
      </div>

      <div className="pdf-viewer__document">
        {error ? (
          <div className="pdf-viewer__error" role="alert">
            <p>PDF 加载失败：{error}</p>
            <p className="pdf-viewer__error-hint">
              请确认文件可访问，或重新导入。
            </p>
          </div>
        ) : (
          <Document
            file={file}
            onLoadSuccess={onLoadSuccess}
            onLoadError={onLoadError}
            loading={<div className="pdf-viewer__loading">加载 PDF 中…</div>}
          >
            <Page
              pageNumber={pageNumber}
              scale={scale}
              renderTextLayer
              renderAnnotationLayer
            />
          </Document>
        )}
        {loading && !error ? (
          <div className="pdf-viewer__loading-overlay" aria-hidden="true" />
        ) : null}
      </div>
    </div>
  );
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function round2(value: number): number {
  return Math.round(value * 100) / 100;
}

export default PdfViewer;
