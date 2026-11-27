import { useRef, useState } from "react";

// charts.tsx — компактные SVG-графики для админ-панели без внешних зависимостей
// (в web/package.json только react). Общее: сетка, оси с подписями, ховер
// с перекрестьем/подсветкой и тултипом с координатами и значениями.

export type Point = { label: string; value: number; extra?: string };

const fmtInt = (n: number) => new Intl.NumberFormat("ru-RU").format(Math.round(n));
const fmtFrac = (n: number) => new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 }).format(n);

function niceMax(v: number): number {
  if (v <= 5) return 5;
  const pow = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 5, 10]) {
    if (v <= m * pow) return m * pow;
  }
  return 10 * pow;
}

function ChartFrame({ title, hint, children, legend }: {
  title: string;
  hint?: string;
  children: (w: number, h: number) => React.ReactNode;
  legend?: React.ReactNode;
}) {
  const W = 460, H = 190;
  return (
    <div className="chart-card">
      <div className="chart-head">
        <div>
          <div className="chart-title">{title}</div>
          {hint && <div className="muted small">{hint}</div>}
        </div>
        {legend}
      </div>
      {children(W, H)}
    </div>
  );
}

function Tooltip({ x, y, lines }: { x: string | number; y: number; lines: string[] }) {
  return (
    <div className="chart-tip" style={{ left: x, top: y }}>
      {lines.map((l, i) => (
        <div key={i} className={i === 0 ? "chart-tip-head" : undefined}>{l}</div>
      ))}
    </div>
  );
}

// smoothPath — сглаженная кривая (Catmull-Rom → кубические Безье).
function smoothPath(pts: { x: number; y: number }[]): string {
  if (pts.length < 2) return pts.length ? `M${pts[0].x},${pts[0].y}` : "";
  let d = `M${pts[0].x},${pts[0].y}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(0, i - 1)], p1 = pts[i], p2 = pts[i + 1], p3 = pts[Math.min(pts.length - 1, i + 2)];
    const c1x = p1.x + (p2.x - p0.x) / 6, c1y = p1.y + (p2.y - p0.y) / 6;
    const c2x = p2.x - (p3.x - p1.x) / 6, c2y = p2.y - (p3.y - p1.y) / 6;
    d += ` C${c1x},${c1y} ${c2x},${c2y} ${p2.x},${p2.y}`;
  }
  return d;
}

// AreaChart — сглаженная область/линия с перекрестьем и тултипом.
// second — необязательный второй ряд (нормируется к своему максимуму),
// рисуется пунктиром (например, средняя задержка мс поверх числа запросов).
export function AreaChart({ title, hint, data, color = "#58a6ff", second, secondColor = "#d29922", secondName }: {
  title: string;
  hint?: string;
  data: Point[];
  color?: string;
  second?: Point[];
  secondColor?: string;
  secondName?: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<SVGSVGElement>(null);
  const W = 460, H = 190, ML = 40, MR = 40, MT = 12, MB = 26;
  const iw = W - ML - MR, ih = H - MT - MB;
  const maxV = niceMax(Math.max(1, ...data.map((p) => p.value)));
  const maxS = second?.length ? Math.max(1, ...second.map((p) => p.value)) : 1;
  const n = data.length;
  const xAt = (i: number) => ML + (n <= 1 ? iw / 2 : (i * iw) / (n - 1));
  const yAt = (v: number) => MT + ih - (v / maxV) * ih;
  const pts = data.map((p, i) => ({ x: xAt(i), y: yAt(p.value) }));
  const line = smoothPath(pts);
  const area = pts.length ? `${line} L${pts[pts.length - 1].x},${MT + ih} L${pts[0].x},${MT + ih} Z` : "";
  const sPts = (second || []).map((p, i) => ({ x: xAt(i), y: MT + ih - (p.value / maxS) * ih }));
  const gridVals = [0.25, 0.5, 0.75, 1].map((k) => maxV * k);

  const onMove = (e: React.MouseEvent) => {
    const svg = ref.current;
    if (!svg || !n) return;
    const rect = svg.getBoundingClientRect();
    const px = ((e.clientX - rect.left) / rect.width) * W;
    let best = 0, bd = Infinity;
    for (let i = 0; i < n; i++) {
      const d = Math.abs(xAt(i) - px);
      if (d < bd) { bd = d; best = i; }
    }
    setHover(best);
  };

  const step = Math.max(1, Math.ceil(n / 7));
  return (
    <ChartFrame title={title} hint={hint}>
      {(w, h) => (
        <div className="chart-body">
          <svg ref={ref} viewBox={`0 0 ${w} ${h}`} onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
            <defs>
              <linearGradient id={`grad-${title.replace(/\W/g, "")}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={color} stopOpacity="0.35" />
                <stop offset="100%" stopColor={color} stopOpacity="0.03" />
              </linearGradient>
            </defs>
            {gridVals.map((v, i) => (
              <g key={i}>
                <line x1={ML} x2={W - MR} y1={yAt(v)} y2={yAt(v)} stroke="currentColor" strokeOpacity="0.12" />
                <text x={ML - 6} y={yAt(v) + 3} textAnchor="end" fontSize="9" fill="currentColor" opacity="0.55">{fmtInt(v)}</text>
              </g>
            ))}
            {area && <path d={area} fill={`url(#grad-${title.replace(/\W/g, "")})`} />}
            <path d={line} fill="none" stroke={color} strokeWidth="2" />
            {second && <path d={smoothPath(sPts)} fill="none" stroke={secondColor} strokeWidth="1.4" strokeDasharray="4 3" />}
            {data.map((p, i) => (
              i % step === 0 && (
                <text key={i} x={xAt(i)} y={H - 8} textAnchor="middle" fontSize="9" fill="currentColor" opacity="0.55">
                  {p.label}
                </text>
              )
            ))}
            {hover !== null && pts[hover] && (
              <g>
                <line x1={pts[hover].x} x2={pts[hover].x} y1={MT} y2={MT + ih} stroke={color} strokeOpacity="0.5" />
                <circle cx={pts[hover].x} cy={pts[hover].y} r="3.5" fill={color} stroke="var(--card, #161b22)" strokeWidth="1.5" />
                {second && sPts[hover] && (
                  <circle cx={sPts[hover].x} cy={sPts[hover].y} r="3" fill={secondColor} stroke="var(--card, #161b22)" strokeWidth="1.5" />
                )}
              </g>
            )}
          </svg>
          {hover !== null && data[hover] && (
            <Tooltip
              x={Math.min(Math.max(xAt(hover) / W * 100, 8), 72) + "%"}
              y={4}
              lines={[
                data[hover].extra || data[hover].label,
                `${data[hover].value}`,
                second?.[hover] ? `${secondName || "ряд 2"}: ${fmtFrac(second[hover].value)}` : "",
              ].filter(Boolean)}
            />
          )}
        </div>
      )}
    </ChartFrame>
  );
}

// BarChart — столбцы с подсветкой и тултипом (дни, регистрации).
export function BarChart({ title, hint, data, color = "#3fb950", valueName = "" }: {
  title: string;
  hint?: string;
  data: Point[];
  color?: string;
  valueName?: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const W = 460, H = 190, ML = 40, MR = 12, MT = 12, MB = 26;
  const iw = W - ML - MR, ih = H - MT - MB;
  const maxV = niceMax(Math.max(1, ...data.map((p) => p.value)));
  const n = data.length;
  const slot = iw / Math.max(1, n);
  const bw = Math.min(26, slot * 0.62);
  const gridVals = [0.25, 0.5, 0.75, 1].map((k) => maxV * k);
  const step = Math.max(1, Math.ceil(n / 7));
  return (
    <ChartFrame title={title} hint={hint}>
      {(w, h) => (
        <div className="chart-body">
          <svg viewBox={`0 0 ${w} ${h}`} onMouseLeave={() => setHover(null)}>
            {gridVals.map((v, i) => (
              <g key={i}>
                <line x1={ML} x2={W - MR} y1={MT + ih - (v / maxV) * ih} y2={MT + ih - (v / maxV) * ih} stroke="currentColor" strokeOpacity="0.12" />
                <text x={ML - 6} y={MT + ih - (v / maxV) * ih + 3} textAnchor="end" fontSize="9" fill="currentColor" opacity="0.55">{fmtInt(v)}</text>
              </g>
            ))}
            {data.map((p, i) => {
              const x = ML + i * slot + (slot - bw) / 2;
              const bh = Math.max(p.value > 0 ? 2 : 0, (p.value / maxV) * ih);
              const y = MT + ih - bh;
              return (
                <g key={i} onMouseEnter={() => setHover(i)}>
                  <rect x={ML + i * slot} y={MT} width={slot} height={ih} fill="transparent" />
                  <rect x={x} y={y} width={bw} height={bh} rx={Math.min(4, bw / 2)} fill={color}
                    opacity={hover === null || hover === i ? 0.95 : 0.45} />
                </g>
              );
            })}
            {data.map((p, i) => (
              i % step === 0 && (
                <text key={i} x={ML + i * slot + slot / 2} y={H - 8} textAnchor="middle" fontSize="9" fill="currentColor" opacity="0.55">
                  {p.label}
                </text>
              )
            ))}
          </svg>
          {hover !== null && data[hover] && (
            <Tooltip
              x={Math.min(Math.max((ML + hover * slot + slot / 2) / W * 100, 8), 72) + "%"}
              y={4}
              lines={[data[hover].extra || data[hover].label, `${valueName ? valueName + ": " : ""}${fmtInt(data[hover].value)}`]}
            />
          )}
        </div>
      )}
    </ChartFrame>
  );
}

// HBarList — горизонтальные полосы (топ пользователей).
export function HBarList({ title, hint, data }: {
  title: string;
  hint?: string;
  data: { label: string; value: number; extra?: string }[];
}) {
  const maxV = Math.max(1, ...data.map((d) => d.value));
  return (
    <ChartFrame title={title} hint={hint}>
      {() => (
        <div className="chart-body chart-hbars">
          {data.map((d, i) => (
            <div key={i} className="chart-hbar" title={`${d.extra || d.label} — ${fmtInt(d.value)}`}>
              <span className="chart-hbar-label mono" title={d.label}>{d.label || "—"}</span>
              <span className="chart-hbar-track">
                <span className="chart-hbar-fill" style={{ width: `${(d.value / maxV) * 100}%` }} />
              </span>
              <span className="chart-hbar-value">{fmtInt(d.value)}</span>
            </div>
          ))}
          {!data.length && <p className="muted small">нет данных за период</p>}
        </div>
      )}
    </ChartFrame>
  );
}

const STATUS_COLORS: Record<string, string> = {
  ok: "#3fb950",
  redirect: "#58a6ff",
  clientError: "#d29922",
  serverError: "#f85149",
};
const STATUS_LABELS: Record<string, string> = {
  ok: "2xx",
  redirect: "3xx",
  clientError: "4xx",
  serverError: "5xx",
};

// Donut — кольцевая диаграмма статусов с подсветкой сегмента и тултипом.
export function Donut({ title, hint, segments }: {
  title: string;
  hint?: string;
  segments: { key: string; label: string; value: number }[];
}) {
  const [hover, setHover] = useState<string | null>(null);
  const total = segments.reduce((s, x) => s + x.value, 0);
  const R = 62, r = 40, CX = 80, CY = 80;
  let angle = -Math.PI / 2;
  const arcs = segments.filter((s) => s.value > 0).map((s) => {
    const frac = total ? s.value / total : 0;
    const a0 = angle, a1 = angle + frac * Math.PI * 2 - (frac < 1 ? 0.02 : 0);
    angle = angle + frac * Math.PI * 2;
    const large = a1 - a0 > Math.PI ? 1 : 0;
    const p = (a: number, radius: number) => `${CX + radius * Math.cos(a)},${CY + radius * Math.sin(a)}`;
    return {
      key: s.key,
      d: `M${p(a0, R)} A${R},${R} 0 ${large} 1 ${p(a1, R)} L${p(a1, r)} A${r},${r} 0 ${large} 0 ${p(a0, r)} Z`,
      frac,
    };
  });
  return (
    <ChartFrame title={title} hint={hint}>
      {() => (
        <div className="chart-body chart-donut">
          <svg viewBox="0 0 160 160" style={{ width: 160, height: 160 }}>
            {arcs.map((a) => (
              <path
                key={a.key}
                d={a.d}
                fill={STATUS_COLORS[a.key] || "#8b949e"}
                opacity={hover === null || hover === a.key ? 1 : 0.4}
                onMouseEnter={() => setHover(a.key)}
                onMouseLeave={() => setHover(null)}
              />
            ))}
            <text x={CX} y={CY - 2} textAnchor="middle" fontSize="20" fontWeight="600" fill="currentColor">
              {fmtInt(total)}
            </text>
            <text x={CX} y={CY + 16} textAnchor="middle" fontSize="9" fill="currentColor" opacity="0.6">
              {hover ? STATUS_LABELS[hover] + ": " + fmtInt(segments.find((s) => s.key === hover)?.value || 0) : "запросов"}
            </text>
          </svg>
          <div className="chart-legend">
            {segments.map((s) => (
              <div key={s.key} className="chart-legend-row" onMouseEnter={() => setHover(s.key)} onMouseLeave={() => setHover(null)}>
                <span className="chart-dot" style={{ background: STATUS_COLORS[s.key] }} />
                <span>{s.label}</span>
                <span className="muted">{total ? `${Math.round((s.value / total) * 100)}%` : "—"}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </ChartFrame>
  );
}

// bucketLabel — "2026-11-05T13:00:00Z" → "13:00" (часы) или "05.11" (дни).
export function bucketLabel(bucket: string, kind: "hour" | "day"): string {
  // Часы/дни приходят в UTC; подписи на оси дают представление о масштабе,
  // точное время — в тултипе (там локальная зона).
  if (kind === "hour") return bucket.slice(11, 13) + ":00";
  return bucket.slice(8, 10) + "." + bucket.slice(5, 7);
}

// bucketTip — тултип-заголовок с локальным временем.
export function bucketTip(bucket: string): string {
  const d = new Date(bucket);
  if (Number.isNaN(d.getTime())) return bucket;
  return d.toLocaleString("ru-RU", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" });
}
