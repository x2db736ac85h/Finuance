// Ported from the legacy ICONS set (legacy/finuance.html): the same 24×24 stroke paths, as data so
// <Icon> builds SVG elements without HTML strings (PLAN §7.1). Drawn with currentColor; see .ico in base.css.

export type Shape = readonly [
  'path' | 'circle' | 'rect' | 'line' | 'polyline' | 'ellipse',
  Readonly<Record<string, string>>,
];

export const ICONS = {
  search: [
    ['circle', { cx: '11', cy: '11', r: '7' }],
    ['path', { d: 'm20 20-3.5-3.5' }],
  ],
  file: [
    ['path', { d: 'M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z' }],
    ['path', { d: 'M14 3v5h5M9 13h6M9 17h6' }],
  ],
  layers: [
    ['path', { d: 'm12 3 9 5-9 5-9-5z' }],
    ['path', { d: 'm3 13 9 5 9-5' }],
  ],
  upload: [
    ['path', { d: 'M12 15V4' }],
    ['path', { d: 'm7 9 5-5 5 5' }],
    ['path', { d: 'M5 15v3a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-3' }],
  ],
  folder: [['path', { d: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z' }]],
  plus: [['path', { d: 'M12 5v14M5 12h14' }]],
  x: [['path', { d: 'M6 6l12 12M18 6 6 18' }]],
  filter: [['path', { d: 'M4 5h16l-6 7.5V19l-4 1.5v-8z' }]],
  up: [
    ['path', { d: 'M12 19V5' }],
    ['path', { d: 'm6 11 6-6 6 6' }],
  ],
  down: [
    ['path', { d: 'M12 5v14' }],
    ['path', { d: 'm6 13 6 6 6-6' }],
  ],
  columns: [
    ['rect', { x: '3', y: '4', width: '18', height: '16', rx: '2' }],
    ['path', { d: 'M9 4v16M15 4v16' }],
  ],
  download: [
    ['path', { d: 'M12 4v11' }],
    ['path', { d: 'm7 10 5 5 5-5' }],
    ['path', { d: 'M5 19h14' }],
  ],
  sun: [
    ['circle', { cx: '12', cy: '12', r: '4' }],
    [
      'path',
      { d: 'M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4' },
    ],
  ],
  moon: [['path', { d: 'M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z' }]],
  copy: [
    ['rect', { x: '9', y: '9', width: '11', height: '11', rx: '2' }],
    ['path', { d: 'M5 15V6a2 2 0 0 1 2-2h9' }],
  ],
  table: [
    ['rect', { x: '3', y: '4', width: '18', height: '16', rx: '2' }],
    ['path', { d: 'M3 10h18M3 15h18M9 10v10' }],
  ],
  sidebar: [
    ['rect', { x: '3', y: '4', width: '18', height: '16', rx: '2' }],
    ['path', { d: 'M9 4v16' }],
  ],
  cal: [
    ['rect', { x: '4', y: '5', width: '16', height: '15', rx: '2' }],
    ['path', { d: 'M4 10h16M9 3v4M15 3v4' }],
  ],
  tag: [
    ['path', { d: 'M3 12.5V4a1 1 0 0 1 1-1h8.5L21 11.5 12.5 20z' }],
    ['circle', { cx: '7.5', cy: '7.5', r: '1.5' }],
  ],
  pen: [
    ['path', { d: 'M4 20h4L19 9l-4-4L4 16z' }],
    ['path', { d: 'm14 6 4 4' }],
  ],
  shrink: [['path', { d: 'M4 14h6v6M20 10h-6V4M14 10l7-7M3 21l7-7' }]],
  chev: [['path', { d: 'm6 9 6 6 6-6' }]],
  ai: [
    ['path', { d: 'M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z' }],
    ['path', { d: 'M19 15.5l.7 1.8 1.8.7-1.8.7-.7 1.8-.7-1.8-1.8-.7 1.8-.7z' }],
  ],
  send: [
    ['path', { d: 'M12 19V5' }],
    ['path', { d: 'm6 11 6-6 6 6' }],
  ],
  stop: [['rect', { x: '7', y: '7', width: '10', height: '10', rx: '1.5' }]],
  plug: [['path', { d: 'M9 3v4M15 3v4M7 7h10v4a5 5 0 0 1-10 0zM12 16v5' }]],
  cog: [
    ['circle', { cx: '12', cy: '12', r: '3' }],
    [
      'path',
      {
        d: 'M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z',
      },
    ],
  ],
  lock: [
    ['rect', { x: '5', y: '11', width: '14', height: '10', rx: '2' }],
    ['path', { d: 'M8 11V7a4 4 0 0 1 8 0v4' }],
  ],
  expand: [['path', { d: 'M14 4h6v6M10 20H4v-6M20 4l-7 7M4 20l7-7' }]],
  dash: [
    ['rect', { x: '3', y: '3', width: '7.5', height: '9', rx: '1.5' }],
    ['rect', { x: '13.5', y: '3', width: '7.5', height: '5.5', rx: '1.5' }],
    ['rect', { x: '13.5', y: '11.5', width: '7.5', height: '9.5', rx: '1.5' }],
    ['rect', { x: '3', y: '15', width: '7.5', height: '6', rx: '1.5' }],
  ],
  type: [['path', { d: 'M3 7V5h11v2M8.5 5v14M6 19h5M14 13v-1.5h7V13M17.5 11.5V19M16 19h3' }]],
  book: [
    ['path', { d: 'M4 19.5v-15A2.5 2.5 0 0 1 6.5 2H20v20H6.5a2.5 2.5 0 0 1 0-5H20' }],
    [
      'path',
      {
        d: 'M15.4 7.3a2.3 2.3 0 0 0-2.1-1.2c-1.2 0-2.1.7-2.1 1.6 0 2.2 4.4 1.3 4.4 3.6 0 1-.9 1.8-2.3 1.8a2.5 2.5 0 0 1-2.2-1.3M13.3 4.8v1.3M13.3 13.1v1.3',
      },
    ],
  ],
  chart: [
    ['path', { d: 'M3 20h18' }],
    ['path', { d: 'M7 16v-4M12 16V6M17 16v-7' }],
  ],
  left: [['path', { d: 'm15 6-6 6 6 6' }]],
  right: [['path', { d: 'm9 6 6 6-6 6' }]],
  chevup: [['path', { d: 'm6 15 6-6 6 6' }]],
  sliders: [
    ['path', { d: 'M4 7h9M19 7h1M4 17h3M13 17h7' }],
    ['circle', { cx: '16', cy: '7', r: '2.5' }],
    ['circle', { cx: '10', cy: '17', r: '2.5' }],
  ],
  eyeoff: [
    ['path', { d: 'M3 3l18 18' }],
    [
      'path',
      {
        d: 'M10.6 6.1A9.8 9.8 0 0 1 12 6c5 0 9 6 9 6a16 16 0 0 1-2.6 3.2M6.3 7.6C4.2 9.1 3 12 3 12s4 6 9 6a8.6 8.6 0 0 0 3.7-.8',
      },
    ],
  ],
} as const satisfies Record<string, readonly Shape[]>;

/**
 * Icons the legacy app didn't have (Phase 19 and later), drawn in the same style: a 24 × 24 box,
 * 1.75 stroke, round caps. Kept apart so ICONS stays the exact legacy set (Icon.test.tsx).
 */
export const APP_ICONS = {
  // friends
  users: [
    ['circle', { cx: '9', cy: '8', r: '3.5' }],
    ['path', { d: 'M2.5 20a6.5 6.5 0 0 1 13 0' }],
    ['path', { d: 'M16 4.6a3.5 3.5 0 0 1 0 6.8M18 14.2a6.5 6.5 0 0 1 3.5 5.8' }],
  ],
  // the Shared tab: two arrows passing each other
  swap: [
    ['path', { d: 'M4 8h13M13.5 4.5 17 8l-3.5 3.5' }],
    ['path', { d: 'M20 16H7M10.5 12.5 7 16l3.5 3.5' }],
  ],
  // the admin panel's Developer section
  code: [['path', { d: 'm8 7-5 5 5 5M16 7l5 5-5 5M13.5 4.5l-3 15' }]],
  // a note on hover ("how to get in")
  info: [
    ['circle', { cx: '12', cy: '12', r: '9' }],
    ['path', { d: 'M12 11v5.5M12 7.6v.1' }],
  ],
  // a photo for the assistant (receipts)
  camera: [
    [
      'path',
      {
        d: 'M3.5 8.5A1.5 1.5 0 0 1 5 7h2.5L9 5h6l1.5 2H19a1.5 1.5 0 0 1 1.5 1.5v9A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5z',
      },
    ],
    ['circle', { cx: '12', cy: '12.5', r: '3.5' }],
  ],
  // notifications
  bell: [
    ['path', { d: 'M6 9a6 6 0 0 1 12 0c0 6 2.5 7.5 2.5 7.5h-17S6 15 6 9' }],
    ['path', { d: 'M10 20a2 2 0 0 0 4 0' }],
  ],
} as const satisfies Record<string, readonly Shape[]>;

export const ALL_ICONS = { ...ICONS, ...APP_ICONS };

export type IconName = keyof typeof ALL_ICONS;
