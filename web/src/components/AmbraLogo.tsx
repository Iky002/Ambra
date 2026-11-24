import React, { useId } from 'react';

interface AmbraLogoProps {
  size?: number;
  showWordmark?: boolean;
  subtitle?: string;
  className?: string;
}

export const AmbraLogo: React.FC<AmbraLogoProps> = ({
  size = 36,
  showWordmark = false,
  subtitle,
  className = '',
}) => {
  const uid = useId().replace(/:/g, '');
  const leftId = `ambra-left-${uid}`;
  const rightId = `ambra-right-${uid}`;
  const beamId = `ambra-beam-${uid}`;
  const apexId = `ambra-apex-${uid}`;

  return (
    <span className={`inline-flex items-center gap-2.5 select-none ${className}`}>
      <svg
        width={size}
        height={size}
        viewBox="0 0 128 128"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        className="shrink-0 drop-shadow-[0_6px_18px_rgba(245,158,11,0.30)]"
        aria-label="Ambra Logo"
      >
        <defs>
          <linearGradient id={leftId} x1="58" y1="12" x2="26" y2="112" gradientUnits="userSpaceOnUse">
            <stop offset="0%" stopColor="#FEF3C7" />
            <stop offset="32%" stopColor="#FBBF24" />
            <stop offset="72%" stopColor="#EA580C" />
            <stop offset="100%" stopColor="#9A3412" />
          </linearGradient>
          <linearGradient id={rightId} x1="64" y1="16" x2="108" y2="112" gradientUnits="userSpaceOnUse">
            <stop offset="0%" stopColor="#FDE68A" />
            <stop offset="40%" stopColor="#F59E0B" />
            <stop offset="82%" stopColor="#C2410C" />
            <stop offset="100%" stopColor="#7C2D12" />
          </linearGradient>
          <linearGradient id={beamId} x1="36" y1="74" x2="96" y2="96" gradientUnits="userSpaceOnUse">
            <stop offset="0%" stopColor="#FFFBEB" stopOpacity="0.92" />
            <stop offset="55%" stopColor="#FCD34D" stopOpacity="0.88" />
            <stop offset="100%" stopColor="#EA580C" stopOpacity="0.72" />
          </linearGradient>
          <linearGradient id={apexId} x1="64" y1="12" x2="64" y2="56" gradientUnits="userSpaceOnUse">
            <stop offset="0%" stopColor="#FFFFFF" stopOpacity="0.75" />
            <stop offset="100%" stopColor="#FDE68A" stopOpacity="0" />
          </linearGradient>
        </defs>
        <path
          d="M64 14C68.6 14 72.7 16.7 74.6 20.9L108.8 97.6C111.7 104.1 106.9 111.4 99.8 111.4H89.4C85.2 111.4 81.4 108.9 79.7 105.0L56.2 51.6L64 14Z"
          fill={`url(#${rightId})`}
        />
        <path
          d="M53.4 20.9C55.3 16.7 59.4 14 64 14C69.2 14 73.1 18.6 71.7 23.6L47.5 104.3C46.3 108.5 42.4 111.4 38.0 111.4H27.8C20.7 111.4 15.9 104.0 18.9 97.5L53.4 20.9Z"
          fill={`url(#${leftId})`}
        />
        <path
          d="M44.5 76.2C45.9 72.6 49.4 70.2 53.3 70.2H79.6C83.7 70.2 87.3 72.8 88.6 76.7L91.8 86.2C92.5 88.2 91.0 90.2 88.9 90.2H43.1C40.7 90.2 39.1 87.8 40.0 85.6L44.5 76.2Z"
          fill={`url(#${beamId})`}
        />
        <path
          d="M64 14C60.6 14 57.5 15.8 55.6 18.6L45.2 42.4C52.6 45.4 61.8 43.2 68.4 35.8L71.7 23.6C73.1 18.6 69.2 14 64 14Z"
          fill={`url(#${apexId})`}
        />
      </svg>
      {showWordmark && (
        <span className="flex flex-col leading-none">
          <span className="text-lg font-semibold tracking-wider uppercase bg-gradient-to-r from-amber-100 via-amber-300 to-orange-400 bg-clip-text text-transparent">
            AMBRA
          </span>
          {subtitle && <span className="text-[11px] text-zinc-400 mt-0.5 tracking-normal normal-case">{subtitle}</span>}
        </span>
      )}
    </span>
  );
};
