import { Color3 } from "./babylon";
import type { ColourId, Seat, SeatDto } from "./api";

/**
 * Seats and colours, which used to be the same thing.
 *
 * A seat is a place at the table: where you sit, which direction your camera
 * looks from, where your stones start. A colour is what your stones look like.
 * Tying the two together meant "I want to play green" was really "I want the
 * south-west chair", which is not what anyone means by it.
 *
 * The seat still owns the geometry. The colour is decided by the server (see
 * colours.go — preferences, with the opening dice settling any argument) and
 * arrives on each seat in the state.
 */
export interface SeatInfo {
  seat: Seat;
  /** Camera alpha that puts this player behind their own pieces. */
  alpha: number;
  /** Direction of their home corner in the board plane (unit vector). */
  dir: { x: number; z: number };
  label: string;
  /** The colour this seat is currently wearing. */
  colourName: string;
  /** Base hue of this player's stones; each skin renders it differently. */
  hue: Color3;
  /** CSS colour for the HUD, matching `hue`. */
  css: string;
}

const TAU = Math.PI * 2;

export interface ColourInfo {
  id: ColourId;
  name: string;
  css: string;
  hue: Color3;
}

function colour(id: ColourId, name: string, css: string): ColourInfo {
  return { id, name, css, hue: Color3.FromHexString(css) };
}

export const COLOURS: Record<ColourId, ColourInfo> = {
  obsidian: colour("obsidian", "Obsidian", "#4A443E"),
  jade: colour("jade", "Jade", "#3F8C63"),
  amber: colour("amber", "Amber", "#C9A227"),
  ember: colour("ember", "Ember", "#C9884F"),
  azure: colour("azure", "Azure", "#4272B8"),
  plum: colour("plum", "Plum", "#8E5AA8"),
};

export const COLOUR_LIST: ColourInfo[] = [
  COLOURS.obsidian, COLOURS.jade, COLOURS.amber, COLOURS.ember, COLOURS.azure, COLOURS.plum,
];

export function colourInfo(id: string | undefined): ColourInfo {
  return COLOURS[(id ?? "") as ColourId] ?? COLOURS.obsidian;
}

interface SeatPlace {
  seat: Seat;
  alpha: number;
  dir: { x: number; z: number };
  label: string;
  /** The colour this seat wears when nobody asked for anything else. */
  own: ColourId;
}

function place(seat: Seat, degrees: number, label: string, own: ColourId): SeatPlace {
  const a = (degrees / 360) * TAU;
  return { seat, alpha: a, dir: { x: Math.cos(a), z: Math.sin(a) }, label, own };
}

// Angles are the real board directions: west's king starts at -8,0 (due west),
// north-east's at 4,4, and so on round the hexagon.
const PLACES: Record<Seat, SeatPlace> = {
  west: place("west", 180, "West", "obsidian"),
  southwest: place("southwest", -120, "South-West", "jade"),
  southeast: place("southeast", -60, "South-East", "amber"),
  east: place("east", 0, "East", "ember"),
  northeast: place("northeast", 60, "North-East", "azure"),
  northwest: place("northwest", 120, "North-West", "plum"),
};

/**
 * Who is wearing what at the table currently on screen.
 *
 * Module state, deliberately: exactly one game is rendered at a time, and the
 * alternative is threading a palette through the board, the skins, the HUD and
 * every toast that names a player. It is set from the state on every update,
 * so it cannot drift; `clearTablePalette` puts it back to the seat defaults
 * when a game closes, so the next table does not open wearing the last one's
 * colours for a frame.
 */
let palette: Partial<Record<Seat, ColourId>> = {};

export function setTablePalette(seats: readonly SeatDto[]): void {
  const next: Partial<Record<Seat, ColourId>> = {};
  for (const s of seats) {
    if (s.colour) next[s.seat] = s.colour;
  }
  palette = next;
}

export function clearTablePalette(): void {
  palette = {};
}

/** The colour id a seat is wearing right now. */
export function seatColour(seat: Seat): ColourId {
  return palette[seat] ?? PLACES[seat]?.own ?? "obsidian";
}

export function seatInfo(seat: Seat): SeatInfo {
  const p = PLACES[seat] ?? PLACES.west;
  const c = COLOURS[seatColour(p.seat)];
  return {
    seat: p.seat,
    alpha: p.alpha,
    dir: p.dir,
    label: p.label,
    colourName: c.name,
    hue: c.hue,
    css: c.css,
  };
}

/** "West (Obsidian)" — used wherever a player is named in the HUD. */
export function seatName(seat: Seat): string {
  const info = seatInfo(seat);
  return `${info.label} (${info.colourName})`;
}
