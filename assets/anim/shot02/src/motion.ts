import { GEO } from './theme';

/**
 * 镜 2 的运动学：全部为帧号的纯函数。
 *
 * 设计原则（这是本片唯一的"证据"镜头）：
 *   计数器不由时间轴触发，而由球的位置反解。
 *   → 画面与数字不可能错帧，改任何缓动曲线都不会破坏一致性。
 */

export type P = { x: number; y: number };

const clamp01 = (v: number) => (v < 0 ? 0 : v > 1 ? 1 : v);

const easeInOut = (t: number) => (t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2);
const easeOut = (t: number) => 1 - Math.pow(1 - t, 3);

/** 二次贝塞尔 */
const bez = (t: number, p0: P, p1: P, p2: P): P => {
  const u = 1 - t;
  return {
    x: u * u * p0.x + 2 * u * t * p1.x + t * t * p2.x,
    y: u * u * p0.y + 2 * u * t * p1.y + t * t * p2.y,
  };
};

/** 段落边界（30fps，共 600 帧 / 20s） */
export const T = {
  riseEnd: 120, // 0-4s   球在 AI-A 内成形待命
  toHandA: 240, // 4-8s   飞出 AI-A，抵达人A   ← 第 1 次穿越发生在此段
  toHandB: 360, // 8-12s  人A → 人B            ← 第 2 次穿越（人际交接）
  intoB: 480, // 12-16s  人B → AI-B            ← 第 3 次穿越发生在此段
  settle: 600, // 16-20s  AI-B 亮起
};

export const ballPos = (f: number): P => {
  // 段 1：在 AI-A 内部升起（不出边界）
  if (f <= T.riseEnd) {
    const t = easeOut(clamp01(f / T.riseEnd));
    return { x: GEO.insideA.x, y: GEO.insideA.y + 70 - 70 * t };
  }
  // 段 2：AI-A → 人A，上抛弧
  if (f <= T.toHandA) {
    const t = easeInOut((f - T.riseEnd) / (T.toHandA - T.riseEnd));
    return bez(
      t,
      { x: GEO.insideA.x, y: GEO.insideA.y },
      { x: 450, y: 290 },
      { x: GEO.humanA.x, y: GEO.handY },
    );
  }
  // 段 3：人A → 人B，抛物线（人力传递）
  if (f <= T.toHandB) {
    const t = easeInOut((f - T.toHandA) / (T.toHandB - T.toHandA));
    return bez(
      t,
      { x: GEO.humanA.x, y: GEO.handY },
      { x: 900, y: 430 },
      { x: GEO.humanB.x, y: GEO.handY },
    );
  }
  // 段 4：人B → AI-B 内部
  if (f <= T.intoB) {
    const t = easeInOut((f - T.toHandB) / (T.intoB - T.toHandB));
    return bez(
      t,
      { x: GEO.humanB.x, y: GEO.handY },
      { x: 1420, y: 520 },
      { x: GEO.insideB.x, y: GEO.insideB.y },
    );
  }
  // 段 5：定住
  return { x: GEO.insideB.x, y: GEO.insideB.y };
};

/**
 * 反解：球第一次越过某个 x 阈值的帧号。
 * 由于 f >= riseEnd 之后 x 单调递增，二分有效。
 */
export const crossFrame = (threshold: number): number => {
  let lo = T.riseEnd;
  let hi = T.intoB;
  if (ballPos(hi).x < threshold) return T.intoB;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (ballPos(mid).x >= threshold) hi = mid;
    else lo = mid;
  }
  return hi;
};

/** 三个计数点（唯一的真相来源） */
export const MARKS = {
  cross1: crossFrame(GEO.edgeA), // 离开 AI-A
  cross2: T.toHandB, // 抵达人B（人际交接完成）
  cross3: crossFrame(GEO.edgeB), // 进入 AI-B
};

/** 当前计数：由帧号推导，不做累加 */
export const countAt = (f: number): number =>
  (f >= MARKS.cross1 ? 1 : 0) + (f >= MARKS.cross2 ? 1 : 0) + (f >= MARKS.cross3 ? 1 : 0);

/** 数字弹跳强度 0..1（用于 scale） */
export const popAt = (f: number, mark: number): number => {
  if (f < mark) return 0;
  return Math.max(0, 1 - (f - mark) / 10);
};
