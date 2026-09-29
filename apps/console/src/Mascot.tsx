import type { MascotId } from './mockData.ts'

interface MascotProps {
  id: MascotId
  color: string
  size?: number
}

// Simple hand-drawn-style SVG mascots, no external art assets. Each is a
// rounded blob body + face, distinguished mainly by ears/silhouette, so
// they read consistently as "the same character set" even though a given
// business only ever shows one. `color` re-tints the body fill per
// business (see mockData.THEME_COLORS) — the face/expression stays fixed
// ink so it stays legible against any of the theme colors.
export function Mascot({ id, color, size = 96 }: MascotProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 96 96"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="吉祥物"
    >
      <MascotEars id={id} color={color} />
      {/* body */}
      <ellipse cx="48" cy="54" rx="30" ry="28" fill={color} />
      {/* cheeks */}
      <ellipse cx="26" cy="60" rx="6" ry="4" fill="white" opacity="0.35" />
      <ellipse cx="70" cy="60" rx="6" ry="4" fill="white" opacity="0.35" />
      {/* face */}
      <circle cx="38" cy="50" r="3.4" fill="#2c2a28" />
      <circle cx="58" cy="50" r="3.4" fill="#2c2a28" />
      <path
        d="M40 62c2.8 3 13.2 3 16 0"
        stroke="#2c2a28"
        strokeWidth="3"
        strokeLinecap="round"
        fill="none"
      />
      <MascotSnoutDetail id={id} />
    </svg>
  )
}

function MascotEars({ id, color }: { id: MascotId; color: string }) {
  switch (id) {
    case 'fox':
      return (
        <>
          <path d="M24 20 L34 44 L14 40 Z" fill={color} />
          <path d="M72 20 L82 40 L62 44 Z" fill={color} />
        </>
      )
    case 'bear':
      return (
        <>
          <circle cx="22" cy="26" r="11" fill={color} />
          <circle cx="74" cy="26" r="11" fill={color} />
        </>
      )
    case 'cat':
      return (
        <>
          <path d="M20 30 L28 8 L38 32 Z" fill={color} />
          <path d="M76 30 L68 8 L58 32 Z" fill={color} />
        </>
      )
    case 'bird':
      return (
        <>
          <ellipse cx="48" cy="20" rx="10" ry="8" fill={color} />
        </>
      )
  }
}

function MascotSnoutDetail({ id }: { id: MascotId }) {
  if (id === 'bird') {
    return <path d="M42 56 L48 62 L54 56 Z" fill="#ffb84c" />
  }
  return null
}
