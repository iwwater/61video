import { FormEvent, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";

export function SearchPanel() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [keyword, setKeyword] = useState(params.get("q") ?? "");

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const q = keyword.trim();
    const sp = new URLSearchParams();
    if (q) sp.set("q", q);
    navigate(`/search?${sp.toString()}`);
  }

  return (
    <form className="search-panel" onSubmit={handleSubmit} role="search">
      <div className="search-panel__form">
        <div className="search-panel__input-wrapper">
          <Search size={16} className="search-panel__search-icon" />
          <input
            className="search-panel__input"
            type="text"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索视频、小说、资源站..."
            aria-label="搜索关键词"
          />
        </div>
        <button className="search-panel__submit" type="submit">
          <Search size={16} className="search-panel__submit-icon" />
          <span className="search-panel__submit-text">搜索</span>
        </button>
      </div>
    </form>
  );
}
