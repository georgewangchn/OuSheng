import React from 'react';
import { AbsoluteFill, useCurrentFrame } from 'remotion';
import { C, FONT_CN, FONT_MONO, GEO, H, W } from './theme';
import { MARKS, ballPos, countAt, popAt } from './motion';

const clamp01 = (v: number) => (v < 0 ? 0 : v > 1 ? 1 : v);
const lerp = (a: number, b: number, t: number) => a + (b - a) * t;

/* ────────────────────────── 背景 ────────────────────────── */

const Grid: React.FC = () => {
  const lines: React.ReactNode[] = [];
  for (let x = 0; x <= W; x += 60) {
    lines.push(<line key={`v${x}`} x1={x} y1={0} x2={x} y2={H} stroke={C.bgGrid} strokeWidth={1} />);
  }
  for (let y = 0; y <= H; y += 60) {
    lines.push(<line key={`h${y}`} x1={0} y1={y} x2={W} y2={y} stroke={C.bgGrid} strokeWidth={1} />);
  }
  return (
    <svg width={W} height={H} style={{ position: 'absolute', inset: 0 }}>
      {lines}
      <radialGradient id="vig" cx="50%" cy="50%" r="72%">
        <stop offset="55%" stopColor="rgba(0,0,0,0)" />
        <stop offset="100%" stopColor="rgba(0,0,0,0.72)" />
      </radialGradient>
      <rect width={W} height={H} fill="url(#vig)" />
    </svg>
  );
};

/* ────────────────────────── AI 面板 ────────────────────────── */

const CODE = [
  'func reconcile(ctx Context) error {',
  '  desired := plan.Desired(ctx)',
  '  current := state.Load()',
  '  diff := plan.Diff(desired, current)',
  '',
  '  for _, d := range diff.Apply {',
  '    if err := exec.Apply(ctx, d); err != nil {',
  '      return fmt.Errorf("apply %s: %w", d.ID, err)',
  '    }',
  '  }',
  '  return nil',
  '}',
];

const Panel: React.FC<{
  box: { x: number; y: number; w: number; h: number };
  tag: string;
  sub: string;
  alive: boolean;
  frame: number;
  flash: number;
  side: 'left' | 'right';
}> = ({ box, tag, sub, alive, frame, flash, side }) => {
  const lineH = 26;
  const scroll = alive ? (frame * 1.15) % (CODE.length * lineH) : 0;
  const borderAlpha = alive ? 0.75 : 0.28;
  const glow = flash * 26;

  return (
    <div
      style={{
        position: 'absolute',
        left: box.x,
        top: box.y,
        width: box.w,
        height: box.h,
        borderRadius: 18,
        border: `2px solid rgba(45,212,191,${borderAlpha + flash * 0.25})`,
        background: alive ? 'rgba(45,212,191,0.055)' : 'rgba(45,212,191,0.018)',
        boxShadow: `0 0 ${glow}px rgba(45,212,191,${0.45 * flash}), inset 0 0 40px rgba(45,212,191,0.04)`,
        overflow: 'hidden',
      }}
    >
      {/* 标题栏 */}
      <div
        style={{
          height: 44,
          borderBottom: '1px solid rgba(45,212,191,0.18)',
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '0 16px',
          fontFamily: FONT_MONO,
        }}
      >
        <div
          style={{
            width: 8,
            height: 8,
            borderRadius: 4,
            background: alive ? C.teal : 'rgba(148,163,184,0.45)',
            boxShadow: alive ? `0 0 10px ${C.teal}` : 'none',
          }}
        />
        <span style={{ color: C.teal, fontSize: 17, letterSpacing: 0.5 }}>{tag}</span>
        <span style={{ color: C.slate, fontSize: 13, opacity: 0.75 }}>{sub}</span>
      </div>

      {/* 代码区 */}
      <div style={{ position: 'relative', height: box.h - 44, overflow: 'hidden', padding: '10px 14px' }}>
        <div style={{ transform: `translateY(${-scroll}px)` }}>
          {[...CODE, ...CODE].map((ln, i) => (
            <div
              key={i}
              style={{
                fontFamily: FONT_MONO,
                fontSize: 12.5,
                lineHeight: `${lineH}px`,
                color: alive ? 'rgba(45,212,191,0.72)' : 'rgba(148,163,184,0.32)',
                whiteSpace: 'pre',
                transition: 'none',
              }}
            >
              {ln || ' '}
            </div>
          ))}
        </div>
        {/* 上下渐隐 */}
        <div
          style={{
            position: 'absolute',
            inset: 0,
            background: `linear-gradient(180deg, rgba(14,17,22,0.85) 0%, transparent 14%, transparent 86%, rgba(14,17,22,0.85) 100%)`,
          }}
        />
      </div>

      {/* 出口标记（球从这里离开） */}
      {side === 'left' && (
        <div
          style={{
            position: 'absolute',
            right: -6,
            top: GEO.handY - box.y - 6,
            width: 12,
            height: 12,
            borderRadius: 6,
            background: 'rgba(45,212,191,0.5)',
            boxShadow: '0 0 14px rgba(45,212,191,0.6)',
          }}
        />
      )}
    </div>
  );
};

/* ────────────────────────── 人 ────────────────────────── */

const Human: React.FC<{
  x: number;
  footY: number;
  label: string;
  ball: { x: number; y: number };
  frame: number;
}> = ({ x, footY, label, ball, frame }) => {
  const natural = { x: x + 30, y: footY - 80 };
  const d = Math.hypot(ball.x - natural.x, ball.y - natural.y);
  const reach = clamp01(1 - (d - 40) / 130);
  const hand = { x: lerp(natural.x, ball.x, reach), y: lerp(natural.y, ball.y, reach) };
  const shoulder = { x: x + 20, y: footY - 108 };
  const breathe = Math.sin(frame / 26) * 1.2;

  return (
    <g>
      {/* 腿 */}
      <path
        d={`M ${x - 11} ${footY - 52} L ${x - 15} ${footY} M ${x + 11} ${footY - 52} L ${x + 15} ${footY}`}
        stroke={C.amberDim}
        strokeWidth={8}
        strokeLinecap="round"
        fill="none"
      />
      {/* 身体 */}
      <path
        d={`M ${x - 21} ${footY - 108} Q ${x} ${footY - 122} ${x + 21} ${footY - 108}
            L ${x + 16} ${footY - 50} Q ${x} ${footY - 44} ${x - 16} ${footY - 50} Z`}
        fill="rgba(245,158,11,0.85)"
      />
      {/* 头 */}
      <circle cx={x} cy={footY - 136 + breathe} r={20} fill={C.amber} />
      <circle cx={x} cy={footY - 136 + breathe} r={26} fill="none" stroke="rgba(245,158,11,0.25)" strokeWidth={2} />
      {/* 手臂 */}
      <path
        d={`M ${shoulder.x} ${shoulder.y} Q ${(shoulder.x + hand.x) / 2} ${Math.min(shoulder.y, hand.y) - 18} ${hand.x} ${hand.y}`}
        stroke={C.amber}
        strokeWidth={7}
        strokeLinecap="round"
        fill="none"
      />
      {/* 名牌 */}
      <text
        x={x}
        y={footY + 34}
        textAnchor="middle"
        fontFamily={FONT_MONO}
        fontSize={16}
        fill={C.slate}
        opacity={0.8}
      >
        {label}
      </text>
    </g>
  );
};

/* ────────────────────────── 计数 ────────────────────────── */

const Counter: React.FC<{ frame: number }> = ({ frame }) => {
  const n = countAt(frame);
  const pops = [MARKS.cross1, MARKS.cross2, MARKS.cross3].map((m) => popAt(frame, m));
  const pop = Math.max(...pops);
  const scale = 1 + 0.3 * pop;
  const hot = pop > 0.01;

  const lamps = [MARKS.cross1, MARKS.cross2, MARKS.cross3];

  return (
    <div
      style={{
        position: 'absolute',
        right: W - GEO.counter.x,
        top: GEO.counter.y,
        transform: 'translateY(-50%)',
        textAlign: 'right',
        fontFamily: FONT_MONO,
      }}
    >
      <div style={{ fontSize: 15, letterSpacing: 3, color: C.slate, opacity: 0.85, marginBottom: 2 }}>
        人机穿越
      </div>
      <div
        style={{
          fontSize: 132,
          lineHeight: 1,
          fontWeight: 700,
          transform: `scale(${scale})`,
          transformOrigin: 'right center',
          color: hot ? C.white : C.teal,
          textShadow: `0 0 ${28 + 40 * pop}px rgba(45,212,191,${0.35 + 0.4 * pop})`,
          transition: 'none',
        }}
      >
        {n}
      </div>
      <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 10 }}>
        {lamps.map((m, i) => {
          const on = frame >= m;
          const p = popAt(frame, m);
          return (
            <div
              key={i}
              style={{
                width: 34,
                height: 5,
                borderRadius: 3,
                background: on ? C.teal : 'rgba(148,163,184,0.22)',
                boxShadow: on ? `0 0 ${8 + 14 * p}px rgba(45,212,191,0.8)` : 'none',
              }}
            />
          );
        })}
      </div>
    </div>
  );
};

/* ────────────────────────── 穿越闪光 ────────────────────────── */

const Flash: React.FC<{ frame: number; mark: number; x: number; y0: number; y1: number }> = ({
  frame,
  mark,
  x,
  y0,
  y1,
}) => {
  const t = clamp01((frame - mark) / 16);
  if (frame < mark || t >= 1) return null;
  const a = 1 - t;
  return (
    <g>
      <line x1={x} y1={y0} x2={x} y2={y1} stroke={C.white} strokeWidth={2 + 3 * a} opacity={a * 0.9} />
      <circle cx={x} cy={GEO.handY} r={16 + 46 * t} fill="none" stroke={C.teal} strokeWidth={2.5 * a} opacity={a} />
    </g>
  );
};

/* ────────────────────────── 字幕 ────────────────────────── */

const CAPTIONS: { from: number; to: number; text: string }[] = [
  { from: 0, to: 108, text: '一条消息，要从左边的 AI 到右边的 AI。' },
  { from: 112, to: 248, text: '中间站着两个人。第 1 跳：AI 说给人听。' },
  { from: 252, to: 368, text: '第 2 跳：人转述给人。' },
  { from: 372, to: 488, text: '第 3 跳：人说给 AI 听。' },
  { from: 496, to: 600, text: '一条消息，三次穿越。' },
];

const Caption: React.FC<{ frame: number }> = ({ frame }) => {
  const cur = CAPTIONS.find((c) => frame >= c.from && frame < c.to);
  if (!cur) return null;
  const fadeIn = clamp01((frame - cur.from) / 9);
  const fadeOut = clamp01((cur.to - frame) / 9);
  const o = Math.min(fadeIn, fadeOut);
  return (
    <div
      style={{
        position: 'absolute',
        bottom: 74,
        width: '100%',
        textAlign: 'center',
        opacity: o,
        transform: `translateY(${(1 - fadeIn) * 10}px)`,
      }}
    >
      <span
        style={{
          fontFamily: FONT_CN,
          fontSize: 38,
          fontWeight: 500,
          color: C.white,
          letterSpacing: 1.5,
          background: 'rgba(14,17,22,0.55)',
          padding: '10px 26px',
          borderRadius: 8,
        }}
      >
        {cur.text}
      </span>
    </div>
  );
};

/* ────────────────────────── 主镜头 ────────────────────────── */

export const Shot02: React.FC = () => {
  const frame = useCurrentFrame();
  const ball = ballPos(frame);

  // 面板闪光：穿越后 16 帧内衰减
  const flashA = Math.max(0, 1 - (frame - MARKS.cross1) / 16);
  const flashB = Math.max(0, 1 - (frame - MARKS.cross3) / 16);
  const bAlive = frame >= MARKS.cross3;

  // 拖尾
  const trail: React.ReactNode[] = [];
  for (let i = 1; i <= 16; i++) {
    const f = frame - i * 2;
    if (f < 0) break;
    const p = ballPos(f);
    trail.push(
      <circle
        key={i}
        cx={p.x}
        cy={p.y}
        r={15 - i * 0.55}
        fill={C.teal}
        opacity={(1 - i / 16) * 0.26}
      />,
    );
  }

  return (
    <AbsoluteFill style={{ background: C.bg }}>
      <Grid />

      <Panel
        box={GEO.panelA}
        tag="AI-A"
        sub="backend"
        alive
        frame={frame}
        flash={flashA}
        side="left"
      />
      <Panel
        box={GEO.panelB}
        tag="AI-B"
        sub="frontend"
        alive={bAlive}
        frame={frame}
        flash={flashB}
        side="right"
      />

      {/* 人与闪光共用一个 SVG 层 */}
      <svg width={W} height={H} style={{ position: 'absolute', inset: 0 }}>
        <Human x={GEO.humanA.x} footY={GEO.humanA.footY} label="人 A" ball={ball} frame={frame} />
        <Human x={GEO.humanB.x} footY={GEO.humanB.footY} label="人 B" ball={ball} frame={frame} />
        <Flash frame={frame} mark={MARKS.cross1} x={GEO.edgeA} y0={GEO.panelA.y - 30} y1={GEO.panelA.y + GEO.panelA.h + 30} />
        <Flash frame={frame} mark={MARKS.cross3} x={GEO.edgeB} y0={GEO.panelB.y - 30} y1={GEO.panelB.y + GEO.panelB.h + 30} />
        {trail}
      </svg>

      {/* 球 */}
      <div
        style={{
          position: 'absolute',
          left: ball.x - 17,
          top: ball.y - 17,
          width: 34,
          height: 34,
          borderRadius: 17,
          background: `radial-gradient(circle at 34% 30%, #ffffff 0%, ${C.teal} 42%, #0f766e 100%)`,
          boxShadow: `0 0 22px rgba(45,212,191,0.85), 0 0 60px rgba(45,212,191,0.35)`,
        }}
      />

      <Counter frame={frame} />
      <Caption frame={frame} />
    </AbsoluteFill>
  );
};
