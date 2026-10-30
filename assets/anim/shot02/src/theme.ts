export const C = {
  bg: '#0E1116',
  bgGrid: 'rgba(148,163,184,0.07)',
  teal: '#2DD4BF',
  tealDim: 'rgba(45,212,191,0.35)',
  tealGhost: 'rgba(45,212,191,0.06)',
  amber: '#F59E0B',
  amberDim: 'rgba(245,158,11,0.5)',
  slate: '#94A3B8',
  white: '#E8EEF5',
  red: '#EF4444',
};

export const FONT_CN = `'PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif`;
export const FONT_MONO = `'SF Mono','Menlo','JetBrains Mono',monospace`;

/** 画布与镜头参数 */
export const W = 1920;
export const H = 1080;

/** 关键坐标（圆心 / 锚点） */
export const GEO = {
  panelA: { x: 120, y: 300, w: 360, h: 380 },
  panelB: { x: 1440, y: 300, w: 360, h: 380 },
  humanA: { x: 640, footY: 840 },
  humanB: { x: 1160, footY: 840 },
  counter: { x: 1660, y: 150 },
  /** 面板内部中心（球在 AI 内的位置） */
  insideA: { x: 300, y: 490 },
  insideB: { x: 1620, y: 490 },
  /** 手部高度 */
  handY: 745,
  /** 面板左右边界 */
  edgeA: 480,
  edgeB: 1440,
};
