import { Music4 } from "lucide-react";

type Props = {
  src: string;
  poster?: string;
  title: string;
  onFirstPlay?: () => void;
};

export function AudioPlayer({ src, poster, title, onFirstPlay }: Props) {
  return (
    <div className="audio-player">
      <div
        className="audio-player__hero"
        style={poster ? { backgroundImage: `url(${poster})` } : undefined}
      >
        <div className="audio-player__overlay" />
        <div className="audio-player__content">
          <span className="audio-player__icon" aria-hidden="true">
            <Music4 size={28} />
          </span>
          <div className="audio-player__meta">
            <span className="audio-player__eyebrow">Audio</span>
            <strong className="audio-player__title">{title}</strong>
          </div>
        </div>
      </div>
      <div className="audio-player__controls">
        <audio
          controls
          preload="metadata"
          src={src}
          className="audio-player__native"
          onPlay={onFirstPlay}
        />
      </div>
    </div>
  );
}
