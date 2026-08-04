import { Color3 } from "./babylon";
import type { Seat } from "./api";

/**
 * Everything the client needs to know about a seat. The name is the compass
 * direction of that player's home corner, which is also the angle their camera
 * looks from — so one table drives both the 3D scene and the HUD.
 */
export interface SeatInfo {
  seat: Seat;
  /** Camera alpha that puts this player behind their own pieces. */
  alpha: number;
  /** Direction of their home corner in the board plane (unit vector). */
  dir: { x: number; z: number };
  label: string;
  colourName: string;
  /** Base hue of this player's stones; each skin renders it differently. */
  hue: Color3;
  /** CSS colour for the HUD, matching `hue`. */
  css: string;
}

const TAU = Math.PI * 2;

function make(seat: Seat, degrees: number, label: string, colourName: string, css: string): SeatInfo {
  const a = (degrees / 360) * TAU;
  return {
    seat,
    alpha: a,
    dir: { x: Math.cos(a), z: Math.sin(a) },
    label,
    colourName,
    hue: Color3.FromHexString(css),
    css,
  };
}

// Angles are the real board directions: west's king starts at -8,0 (due west),
// north-east's at 4,4, and so on round the hexagon.
export const SEATS: Record<Seat, SeatInfo> = {
  west: make("west", 180, "West", "Obsidian", "#4A443E"),
  southwest: make("southwest", -120, "South-West", "Jade", "#3F8C63"),
  southeast: make("southeast", -60, "South-East", "Amber", "#C9A227"),
  east: make("east", 0, "East", "Ember", "#C9884F"),
  northeast: make("northeast", 60, "North-East", "Azure", "#4272B8"),
  northwest: make("northwest", 120, "North-West", "Plum", "#8E5AA8"),
};

export function seatInfo(seat: Seat): SeatInfo {
  return SEATS[seat] ?? SEATS.west;
}

/** "West (Obsidian)" — used wherever a player is named in the HUD. */
export function seatName(seat: Seat): string {
  const info = seatInfo(seat);
  return `${info.label} (${info.colourName})`;
}
