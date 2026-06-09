type SpiderIconProps = {
  size?: number;
  className?: string;
};

export function SpiderIcon({ size = 16, className }: SpiderIconProps) {
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M12 7v3" />
      <path d="M9 5.5 7.5 3" />
      <path d="M15 5.5 16.5 3" />
      <path d="M9 10.5 4.5 8" />
      <path d="M15 10.5 19.5 8" />
      <path d="M8.5 13.5 3 13" />
      <path d="M15.5 13.5 21 13" />
      <path d="M9 16 5 20" />
      <path d="M15 16 19 20" />
      <ellipse cx="12" cy="14" rx="4" ry="5" />
      <circle cx="12" cy="8" r="2.5" />
    </svg>
  );
}
