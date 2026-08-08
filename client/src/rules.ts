// The rules, illustrated.
//
// Every diagram is drawn from the *real* board graph the server sends, not
// from a hand-made copy of it. A picture of the rules that has quietly drifted
// from the rules is worse than no picture, and hand-drawn boards drift the
// moment anybody touches the layout.

import type { BoardDto, BoardNode } from "./api";
import { BOARDS, DEFAULT_BOARD } from "./theme";

const NS = "http://www.w3.org/2000/svg";

/** A stone to draw on a diagram: which field, what value, whose side. */
interface Stone {
  node: string;
  value: number;
  side: "you" | "them";
}

interface Diagram {
  /** Fields to draw a stone on. */
  stones?: Stone[];
  /** A path of fields, drawn as an arrow through them. */
  path?: string[];
  /** Fields to ring as reachable. */
  lit?: string[];
  /** Fields to ring in red as a strike. */
  strike?: string[];
  /** Draw only the gold king-only spokes, not every edge. */
  goldOnly?: boolean;
}

export interface RuleSection {
  title: string;
  body: string[];
  diagram?: Diagram;
}

/**
 * The rules as the engine actually implements them. Where the Dutch rules text
 * and the printed board disagreed, this follows the board — see PROMPT.md.
 */
export const RULES: RuleSection[] = [
  {
    title: "The board and the aim",
    body: [
      "Stones stand on the crossings of a honeycomb, not inside the cells — 55 fields in all, with the Gilded Throne at the centre.",
      "Get your King to the Throne and you win at once. Take every rival's King and you win by being the last one standing.",
    ],
    diagram: { stones: [{ node: "0,0", value: 1, side: "you" }] },
  },
  {
    title: "Your eight stones",
    body: [
      "One King marked 1, three stones marked 2, and four marked 3.",
      "Everyone starts in the same formation, turned to face their own corner of the board.",
    ],
    diagram: {
      stones: [
        { node: "-8,0", value: 1, side: "you" },
        { node: "-8,-2", value: 3, side: "you" },
        { node: "-7,-1", value: 3, side: "you" },
        { node: "-7,1", value: 3, side: "you" },
        { node: "-8,2", value: 3, side: "you" },
        { node: "-5,-1", value: 2, side: "you" },
        { node: "-4,0", value: 2, side: "you" },
        { node: "-5,1", value: 2, side: "you" },
      ],
    },
  },
  {
    title: "Moving is counting",
    body: [
      "A stone moves exactly as many steps as the number on it — no more, no fewer.",
      "You may turn corners along the way, but you may not cross the same field twice in one move, and you may not jump over anybody.",
      "Here a 3 walks three steps and turns as it goes.",
    ],
    // A real three-step path with a turn in it, out near the west edge where
    // the Throne's star does not sit on top of the drawing.
    diagram: {
      stones: [{ node: "-7,-1", value: 3, side: "you" }],
      path: ["-7,-1", "-8,-2", "-7,-3", "-5,-3"],
      lit: ["-5,-3"],
    },
  },
  {
    title: "The Throne is the King's alone",
    body: [
      "Six golden spokes lead to the Throne, and only a King may set foot on them or on the Throne itself.",
      "Everyone else has to work around the middle.",
    ],
    diagram: {
      goldOnly: true,
      stones: [{ node: "2,0", value: 1, side: "you" }],
      lit: ["0,0"],
    },
  },
  {
    title: "Taking a stone",
    body: [
      "End your move exactly on a rival's stone and you take it. It is set aside beside the board and plays no further part.",
      "A stone you can take is circled in red — click it and it is yours.",
    ],
    diagram: {
      stones: [
        { node: "-4,0", value: 2, side: "you" },
        { node: "-2,0", value: 3, side: "them" },
      ],
      path: ["-4,0", "-3,1", "-2,0"],
      strike: ["-2,0"],
    },
  },
  {
    title: "Nobody is struck in the opening round",
    body: [
      "Until everyone has had one turn, no stone may be taken. The player who happens to go first gets no free blood for it.",
      "The status bar says so while it lasts.",
    ],
  },
  {
    title: "Losing your King",
    body: [
      "Lose your King and you are out — but your stones stay exactly where they are, as obstacles for everyone still playing.",
      "The same happens if you are walled in with no legal move at all, or if you let your clock run out.",
    ],
    diagram: {
      stones: [
        { node: "-8,0", value: 1, side: "them" },
        { node: "-5,1", value: 2, side: "you" },
      ],
      path: ["-5,1", "-7,1", "-8,0"],
      strike: ["-8,0"],
    },
  },
  {
    title: "Who opens",
    body: [
      "Everyone rolls a die at the start. Highest opens, and a tie sends only the tied players back to the dice.",
      "You throw your own — click the die when it is your turn.",
    ],
  },
];

/**
 * Draws one diagram as an SVG. Uses the board's own coordinates, so the shape
 * is always the shape being played on.
 */
export function drawDiagram(board: BoardDto, d: Diagram, size = 260): SVGSVGElement {
  const pos = new Map<string, BoardNode>();
  for (const n of board.nodes) pos.set(n.id, n);

  // Frame what the diagram is actually about. The whole board at this size
  // makes a stone about eight pixels across, which is not an illustration of
  // anything — so when a diagram concerns one corner, show that corner.
  const focus = [
    ...(d.stones ?? []).map((s) => s.node),
    ...(d.path ?? []),
    ...(d.lit ?? []),
    ...(d.strike ?? []),
  ]
    .map((id) => pos.get(id))
    .filter((n): n is BoardNode => !!n);

  const xs = board.nodes.map((n) => n.x);
  const ys = board.nodes.map((n) => n.y);
  const boardW = Math.max(...xs) - Math.min(...xs);
  let minX = Math.min(...xs);
  let maxX = Math.max(...xs);
  let minY = Math.min(...ys);
  let maxY = Math.max(...ys);

  if (focus.length) {
    const fx = focus.map((n) => n.x);
    const fy = focus.map((n) => n.y);
    const width = Math.max(...fx) - Math.min(...fx);
    // Only crop when it genuinely helps; a diagram about the whole board
    // should still show the whole board.
    if (width < boardW * 0.7) {
      const room = 2.4;
      minX = Math.min(...fx) - room;
      maxX = Math.max(...fx) + room;
      minY = Math.min(...fy) - room;
      maxY = Math.max(...fy) + room;
      // Keep it roughly square so the cards line up.
      const w = maxX - minX;
      const h = maxY - minY;
      if (w > h) {
        const grow = (w - h) / 2;
        minY -= grow;
        maxY += grow;
      } else {
        const grow = (h - w) / 2;
        minX -= grow;
        maxX += grow;
      }
    }
  }
  const pad = 0.6;
  minX -= pad;
  maxX += pad;
  minY -= pad;
  maxY += pad;

  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", `${minX} ${minY} ${maxX - minX} ${maxY - minY}`);
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(Math.round((size * (maxY - minY)) / (maxX - minX))));
  svg.setAttribute("class", "diagram");
  svg.setAttribute("role", "img");

  const cloth = BOARDS[DEFAULT_BOARD]!;
  const bg = el("rect", {
    x: minX, y: minY, width: maxX - minX, height: maxY - minY,
    rx: 0.6, fill: cloth.cloth,
  });
  svg.appendChild(bg);

  // Edges. Gold ones are the King's spokes and are always drawn.
  for (const e of board.edges) {
    const a = pos.get(e.a);
    const b = pos.get(e.b);
    if (!a || !b) continue;
    if (d.goldOnly && !e.gold) continue;
    svg.appendChild(
      el("line", {
        x1: a.x, y1: a.y, x2: b.x, y2: b.y,
        stroke: e.gold ? "#d9a83a" : "#5e7f86",
        "stroke-width": e.gold ? 0.14 : 0.07,
        "stroke-linecap": "round",
        opacity: e.gold ? 0.95 : 0.5,
      }),
    );
  }

  for (const n of board.nodes) {
    if (d.goldOnly && !n.center && !board.edges.some((e) => e.gold && (e.a === n.id || e.b === n.id))) {
      continue;
    }
    svg.appendChild(
      el("circle", {
        cx: n.x, cy: n.y, r: n.center ? 0.2 : 0.1,
        fill: n.center ? "#d9a83a" : "#8fa3a8",
      }),
    );
  }

  // The route taken, if this diagram shows a move.
  if (d.path && d.path.length > 1) {
    const points = d.path
      .map((id) => pos.get(id))
      .filter((n): n is BoardNode => !!n)
      .map((n) => `${n.x},${n.y}`)
      .join(" ");
    svg.appendChild(
      el("polyline", {
        points, fill: "none", stroke: "#f3d27a", "stroke-width": 0.13,
        "stroke-linecap": "round", "stroke-linejoin": "round",
        "stroke-dasharray": "0.34 0.26",
      }),
    );
  }

  for (const id of d.lit ?? []) {
    const n = pos.get(id);
    if (n) svg.appendChild(el("circle", { cx: n.x, cy: n.y, r: 0.3, fill: "none", stroke: "#7fd4c9", "stroke-width": 0.1 }));
  }
  for (const id of d.strike ?? []) {
    const n = pos.get(id);
    if (n) svg.appendChild(el("circle", { cx: n.x, cy: n.y, r: 0.36, fill: "none", stroke: "#e05a45", "stroke-width": 0.12 }));
  }

  for (const s of d.stones ?? []) {
    const n = pos.get(s.node);
    if (!n) continue;
    const mine = s.side === "you";
    svg.appendChild(
      el("circle", {
        cx: n.x, cy: n.y, r: 0.34,
        fill: mine ? "#4A443E" : "#C9884F",
        stroke: "#15120f", "stroke-width": 0.05,
      }),
    );
    const label = el("text", {
      x: n.x, y: n.y, fill: mine ? "#EFE2C0" : "#26201A",
      "font-size": 0.42, "text-anchor": "middle", "dominant-baseline": "central",
      "font-family": "Georgia, serif",
    });
    label.textContent = String(s.value);
    svg.appendChild(label);
  }
  return svg;
}

function el(name: string, attrs: Record<string, string | number>): SVGElement {
  const node = document.createElementNS(NS, name);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, String(v));
  return node;
}
