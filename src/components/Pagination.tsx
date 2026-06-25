import { memo } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";

type Props = {
  page: number;
  pageSize: number;
  total: number;
  onChange: (p: number) => void;
};

function buildRange(current: number, last: number): (number | "...")[] {
  if (last <= 7) return Array.from({ length: last }, (_, i) => i + 1);

  const result: (number | "...")[] = [1];
  const start = Math.max(2, current - 1);
  const end = Math.min(last - 1, current + 1);

  if (start > 2) result.push("...");
  for (let i = start; i <= end; i++) result.push(i);
  if (end < last - 1) result.push("...");

  result.push(last);
  return result;
}

function PaginationInner({ page, pageSize, total, onChange }: Props) {
  const last = Math.max(1, Math.ceil(total / pageSize));
  if (last <= 1) return null;

  const range = buildRange(page, last);

  return (
    <nav className="pagination" aria-label="分页">
      <button
        className="pagination__btn"
        onClick={() => onChange(page - 1)}
        disabled={page <= 1}
        aria-label="上一页"
      >
        <ChevronLeft size={14} />
      </button>
      {range.map((p, idx) =>
        p === "..." ? (
          <span key={`e${idx}`} className="pagination__btn" aria-hidden>
            ...
          </span>
        ) : (
          <button
            key={p}
            className={`pagination__btn ${p === page ? "is-active" : ""}`}
            onClick={() => onChange(p)}
            aria-current={p === page ? "page" : undefined}
          >
            {p}
          </button>
        )
      )}
      <button
        className="pagination__btn"
        onClick={() => onChange(page + 1)}
        disabled={page >= last}
        aria-label="下一页"
      >
        <ChevronRight size={14} />
      </button>
    </nav>
  );
}

export const Pagination = memo(PaginationInner);
