function escapeXml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')
}

function toSvgDataUrl(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`
}

export interface PosterArtOptions {
  title: string
  subtitle?: string
  year?: number
  badge?: string
  rating?: number
  palette: [string, string, string]
}

export interface BackdropArtOptions {
  title: string
  subtitle?: string
  year?: number
  palette: [string, string, string]
}

/**
 * 生成 2:3 电影级渐变排版 SVG 海报 Data URL（100% 离线秒开、永不裂图）
 */
export function makePosterDataUrl(opts: PosterArtOptions): string {
  const [c1, c2, accent] = opts.palette
  const title = escapeXml(opts.title)
  const subtitle = opts.subtitle ? escapeXml(opts.subtitle.slice(0, 34)) : ''
  const yearStr = opts.year ? String(opts.year) : ''
  const badge = opts.badge ? escapeXml(opts.badge) : ''
  const ratingStr = opts.rating ? `★ ${opts.rating.toFixed(1)}` : ''

  // 根据标题长度自适应字号
  const titleFontSize = opts.title.length > 8 ? 30 : opts.title.length > 5 ? 36 : 42

  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 600" width="400" height="600">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="${c1}"/>
      <stop offset="55%" stop-color="${c2}"/>
      <stop offset="100%" stop-color="#07070c"/>
    </linearGradient>
    <radialGradient id="orb1" cx="78%" cy="22%" r="55%">
      <stop offset="0%" stop-color="${accent}" stop-opacity="0.52"/>
      <stop offset="100%" stop-color="${accent}" stop-opacity="0"/>
    </radialGradient>
    <radialGradient id="orb2" cx="20%" cy="76%" r="50%">
      <stop offset="0%" stop-color="${c1}" stop-opacity="0.45"/>
      <stop offset="100%" stop-color="#000000" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="fade" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#09090d" stop-opacity="0.05"/>
      <stop offset="55%" stop-color="#09090d" stop-opacity="0.25"/>
      <stop offset="100%" stop-color="#09090d" stop-opacity="0.96"/>
    </linearGradient>
  </defs>
  <rect width="400" height="600" fill="url(#bg)"/>
  <rect width="400" height="600" fill="url(#orb1)"/>
  <rect width="400" height="600" fill="url(#orb2)"/>
  <circle cx="310" cy="140" r="118" fill="none" stroke="${accent}" stroke-opacity="0.22" stroke-width="1.5"/>
  <circle cx="310" cy="140" r="78" fill="none" stroke="#ffffff" stroke-opacity="0.1" stroke-width="1"/>
  <path d="M 0 360 Q 200 290 400 350" fill="none" stroke="${accent}" stroke-opacity="0.24" stroke-width="1.5"/>
  <rect width="400" height="600" fill="url(#fade)"/>
  <rect x="20" y="20" width="360" height="560" rx="14" fill="none" stroke="#ffffff" stroke-opacity="0.09" stroke-width="1"/>
  ${
    badge
      ? `<g transform="translate(32, 34)">
    <rect width="${Math.max(76, badge.length * 10 + 22)}" height="28" rx="8" fill="${accent}" fill-opacity="0.24" stroke="${accent}" stroke-opacity="0.55" stroke-width="1"/>
    <text x="12" y="19" fill="#fef3c7" font-family="system-ui,-apple-system,sans-serif" font-size="12" font-weight="700" letter-spacing="1">${badge}</text>
  </g>`
      : ''
  }
  ${
    ratingStr
      ? `<text x="366" y="53" text-anchor="end" fill="#fbbf24" font-family="system-ui,-apple-system,sans-serif" font-size="14" font-weight="700">${ratingStr}</text>`
      : ''
  }
  <text x="34" y="458" fill="#ffffff" font-family="system-ui,-apple-system,'PingFang SC','Microsoft YaHei',sans-serif" font-size="${titleFontSize}" font-weight="800" letter-spacing="0.5">${title}</text>
  ${
    subtitle
      ? `<text x="34" y="492" fill="#d4d4d8" fill-opacity="0.85" font-family="system-ui,-apple-system,sans-serif" font-size="14" font-weight="500" letter-spacing="0.6">${subtitle}</text>`
      : ''
  }
  <line x1="34" y1="516" x2="112" y2="516" stroke="${accent}" stroke-width="2.5" stroke-linecap="round"/>
  <text x="34" y="544" fill="#a1a1aa" font-family="ui-monospace,SFMono-Regular,monospace" font-size="12" letter-spacing="1.6">AMBRA CINEMA${yearStr ? ` · ${yearStr}` : ''}</text>
</svg>`

  return toSvgDataUrl(svg)
}

/**
 * 生成 16:9 沉浸式电影背景图 SVG Data URL
 */
export function makeBackdropDataUrl(opts: BackdropArtOptions): string {
  const [c1, c2, accent] = opts.palette
  const title = escapeXml(opts.title)
  const subtitle = opts.subtitle ? escapeXml(opts.subtitle.slice(0, 42)) : ''
  const yearStr = opts.year ? String(opts.year) : ''

  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 720" width="1280" height="720">
  <defs>
    <linearGradient id="bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#09090d"/>
      <stop offset="42%" stop-color="${c1}"/>
      <stop offset="82%" stop-color="${c2}"/>
      <stop offset="100%" stop-color="#07070c"/>
    </linearGradient>
    <radialGradient id="glowA" cx="76%" cy="32%" r="52%">
      <stop offset="0%" stop-color="${accent}" stop-opacity="0.55"/>
      <stop offset="60%" stop-color="${c2}" stop-opacity="0.18"/>
      <stop offset="100%" stop-color="#000000" stop-opacity="0"/>
    </radialGradient>
    <radialGradient id="glowB" cx="55%" cy="80%" r="48%">
      <stop offset="0%" stop-color="${c1}" stop-opacity="0.4"/>
      <stop offset="100%" stop-color="#000000" stop-opacity="0"/>
    </radialGradient>
  </defs>
  <rect width="1280" height="720" fill="url(#bg)"/>
  <rect width="1280" height="720" fill="url(#glowA)"/>
  <rect width="1280" height="720" fill="url(#glowB)"/>
  <g opacity="0.22" stroke="${accent}" fill="none">
    <circle cx="960" cy="270" r="240" stroke-width="1.5"/>
    <circle cx="960" cy="270" r="165" stroke-width="1" stroke-dasharray="6 8"/>
    <circle cx="960" cy="270" r="320" stroke-width="0.8" stroke-opacity="0.5"/>
  </g>
  <path d="M 0 560 Q 480 430 1280 510" fill="none" stroke="${accent}" stroke-opacity="0.28" stroke-width="2"/>
  <path d="M 0 600 Q 540 490 1280 570" fill="none" stroke="#ffffff" stroke-opacity="0.08" stroke-width="1.2"/>
  <text x="1210" y="610" text-anchor="end" fill="#ffffff" fill-opacity="0.14" font-family="system-ui,-apple-system,'PingFang SC',sans-serif" font-size="68" font-weight="900" letter-spacing="2">${title}</text>
  ${
    subtitle || yearStr
      ? `<text x="1210" y="652" text-anchor="end" fill="#e4e4e7" fill-opacity="0.32" font-family="ui-monospace,SFMono-Regular,monospace" font-size="18" letter-spacing="3">${subtitle}${subtitle && yearStr ? ' · ' : ''}${yearStr}</text>`
      : ''
  }
</svg>`

  return toSvgDataUrl(svg)
}

/**
 * 生成 16:9 媒体库封面图 SVG Data URL
 */
export function makeLibraryThumbDataUrl(
  name: string,
  subtitle: string,
  palette: [string, string, string],
): string {
  const [c1, c2, accent] = palette
  const safeName = escapeXml(name)
  const safeSub = escapeXml(subtitle)

  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 360" width="640" height="360">
  <defs>
    <linearGradient id="lbg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="${c1}"/>
      <stop offset="65%" stop-color="${c2}"/>
      <stop offset="100%" stop-color="#09090d"/>
    </linearGradient>
    <radialGradient id="lglow" cx="80%" cy="25%" r="60%">
      <stop offset="0%" stop-color="${accent}" stop-opacity="0.5"/>
      <stop offset="100%" stop-color="#000000" stop-opacity="0"/>
    </radialGradient>
  </defs>
  <rect width="640" height="360" fill="url(#lbg)"/>
  <rect width="640" height="360" fill="url(#lglow)"/>
  <circle cx="520" cy="95" r="110" fill="none" stroke="${accent}" stroke-opacity="0.25" stroke-width="1.5"/>
  <text x="580" y="290" text-anchor="end" fill="#ffffff" fill-opacity="0.16" font-family="system-ui,-apple-system,'PingFang SC',sans-serif" font-size="56" font-weight="900">${safeName}</text>
  <text x="580" y="324" text-anchor="end" fill="#e4e4e7" fill-opacity="0.4" font-family="ui-monospace,SFMono-Regular,monospace" font-size="14" letter-spacing="2">${safeSub}</text>
</svg>`

  return toSvgDataUrl(svg)
}

/**
 * 生成圆形头像 SVG Data URL（用于用户与演职员）
 */
export function makeAvatarDataUrl(name: string, palette: [string, string]): string {
  const [c1, c2] = palette
  const initial = escapeXml((name || 'A').trim().slice(0, 1).toUpperCase())
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" width="200" height="200">
  <defs>
    <linearGradient id="av" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="${c1}"/>
      <stop offset="100%" stop-color="${c2}"/>
    </linearGradient>
  </defs>
  <rect width="200" height="200" fill="url(#av)"/>
  <circle cx="100" cy="100" r="88" fill="none" stroke="#ffffff" stroke-opacity="0.14" stroke-width="2"/>
  <text x="100" y="118" text-anchor="middle" fill="#fef3c7" font-family="system-ui,-apple-system,'PingFang SC',sans-serif" font-size="76" font-weight="800">${initial}</text>
</svg>`
  return toSvgDataUrl(svg)
}
