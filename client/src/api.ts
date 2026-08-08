// Typed client for the Kingsreach REST API (see PROMPT.md §3.2).
// The server is authoritative: this module never derives rules or geometry.

export interface BoardNode { id: string; x: number; y: number; center: boolean; }
export interface BoardEdge { a: string; b: string; gold: boolean; }
export interface BoardDto { nodes: BoardNode[]; edges: BoardEdge[]; }

export type Seat = "west" | "southwest" | "southeast" | "east" | "northeast" | "northwest";
/** Which seat(s) the caller controls; "all" means the whole table (offline). */
export type Control = Seat | "all" | "";

export interface PieceDto {
  id: string;
  owner: Seat;
  value: number;
  node: string;
  captured: boolean;
}

export interface SeatDto {
  seat: Seat;
  skin: string;
  taken: boolean;
  you: boolean;
  out: boolean;
  cause?: string;
  name?: string;
  avatar?: string;
  country?: string;
  /** Hall-of-champions place, 0 or absent for everyone else. */
  rank?: number;
  /** Played by the computer, and at which difficulty. */
  bot?: boolean;
  level?: string;
}

export interface DiceThrow {
  seat: Seat;
  value: number;
}

export interface MoveRecord {
  piece: string;
  from: string;
  to: string;
  path: string[];
  captured: string;
}

export interface GameState {
  gameId: string;
  code: string;
  /** Table name, as it appears in the lobby browser. */
  name: string;
  mode: "online" | "offline";
  status: "waiting" | "active" | "finished";
  players: number;
  seats: SeatDto[];
  you: Control;
  turn: Seat;
  winner: Seat | "";
  winReason: string;
  /** "roll" until the dice decide who opens, then "play". */
  phase: "roll" | "play";
  version: number;
  ply: number;
  /** False during the opening round, when Kendo forbids strikes. */
  capturing: boolean;
  dice?: DiceThrow[][];
  /** Seats that still owe a throw. */
  pending?: Seat[];
  /** True when you (or the table you run) may throw right now. */
  yourRoll: boolean;
  pieces: PieceDto[];
  lastMove: MoveRecord | null;
  token?: string;
  /** When the player on the move runs out of time (RFC 3339); "" for no clock. */
  deadline?: string;
  /** Set on a finished game once somebody has asked for another one. */
  rematchId?: string;
  /** What was last called out at this table, if it was recent. */
  taunt?: TauntSent;
}

/** A state plus the die just thrown, returned by the roll endpoint. */
export interface RollResult extends GameState {
  rolledSeat: Seat;
  rolled: number;
}

export interface MoveOption {
  to: string;
  path: string[];
  /** Id of the piece standing there — set only when this move is a strike. */
  captures?: string;
}

export interface CatalogItem {
  id: string;
  kind: "skin" | "env" | "board";
  name: string;
  desc: string;
  needPlays: number;
  needWins: number;
}

export interface Profile {
  token: string;
  kind: "steam" | "guest";
  name: string;
  avatar: string;
  country: string;
  /** Guests keep no progress between visits. */
  persistent: boolean;
  gamesPlayed: number;
  wins: number;
  equippedSkin: string;
  equippedEnv: string;
  equippedBoard: string;
  unlocked: string[];
}

/** A table in the lobby browser. */
export interface Lobby {
  gameId: string;
  name: string;
  host: string;
  country: string;
  players: number;
  taken: number;
  locked: boolean;
  age: number;
  yours: boolean;
}

/** A line you can call out to the table. */
export interface TauntOption {
  id: string;
  text: string;
  sound: string;
}

/** A taunt somebody just called out. */
export interface TauntSent {
  seat: Seat;
  id: string;
  text: string;
  at: string;
  /** Changes per send, so a repeat of the same line is not mistaken for a poll echo. */
  nonce: string;
}

/** One line of the hall of champions. */
export interface RankEntry {
  rank?: number;
  name: string;
  avatar?: string;
  country?: string;
  wins: number;
  played: number;
  you?: boolean;
}

export interface Standings {
  minWins: number;
  entries: RankEntry[];
  you?: RankEntry;
  /** Victories still needed to appear; 0 once you are on it, or cannot be. */
  winsNeeded: number;
}

/** A table already under way — shown, but never joinable. */
export interface RunningTable {
  gameId: string;
  name: string;
  host: string;
  country: string;
  players: number;
  ply: number;
  minutes: number;
  /** How many seats the computer is playing. */
  bots: number;
  /** You hold a seat here — a game you walked away from, still going. */
  yours?: boolean;
}

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

async function request<T>(path: string, body?: unknown): Promise<T | null> {
  const init: RequestInit = body === undefined
    ? {}
    : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) };
  const res = await fetch(path, init);
  if (res.status === 204) return null; // polling: nothing changed
  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    /* empty body */
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error ?? `server error (HTTP ${res.status})`;
    throw new ApiError(msg, res.status);
  }
  return data as T;
}

export const api = {
  board: () => request<BoardDto>("/api/board") as Promise<BoardDto>,
  catalog: async () => (await request<{ items: CatalogItem[] }>("/api/catalog"))!.items,
  config: () =>
    request<{ steam: boolean; unlockAll: boolean }>("/api/config") as Promise<{
      steam: boolean;
      unlockAll: boolean;
    }>,
  /** The country the server guesses from your address; "" when it cannot tell. */
  geo: async () => (await request<{ country: string }>("/api/geo"))?.country ?? "",

  createProfile: (name: string, country: string) =>
    request<Profile>("/api/profile", { name, country }) as Promise<Profile>,
  getProfile: (token: string) => request<Profile>(`/api/profile?token=${encodeURIComponent(token)}`) as Promise<Profile>,
  updateProfile: (token: string, name: string, country: string) =>
    request<Profile>("/api/profile/update", { token, name, country }) as Promise<Profile>,
  equip: (token: string, what: { skin?: string; env?: string; board?: string }) =>
    request<Profile>("/api/profile/equip", { token, ...what }) as Promise<Profile>,

  rematch: (id: string, token: string) =>
    request<GameState>(`/api/games/${id}/rematch`, { token }) as Promise<GameState>,

  tauntList: () =>
    request<{ taunts: TauntOption[]; gapMs: number; perGame: number }>("/api/taunts") as Promise<{
      taunts: TauntOption[];
      gapMs: number;
      perGame: number;
    }>,
  taunt: (id: string, token: string, taunt: string) =>
    request<{ sent: string }>(`/api/games/${id}/taunt`, { token, id: taunt }),

  leaderboard: (token: string) =>
    request<Standings>(`/api/leaderboard?token=${encodeURIComponent(token)}`) as Promise<Standings>,

  lobbies: (token: string) =>
    request<{ lobbies: Lobby[]; running: RunningTable[] }>(
      `/api/lobbies?token=${encodeURIComponent(token)}`,
    ) as Promise<{ lobbies: Lobby[]; running: RunningTable[] }>,

  createGame: (opts: {
    mode: "online" | "offline";
    players: number;
    profile: string;
    name?: string;
    password?: string;
  }) => request<GameState>("/api/games", opts) as Promise<GameState>,
  joinGame: (gameId: string, password: string, profile: string) =>
    request<GameState>("/api/games/join", { gameId, password, profile }) as Promise<GameState>,
  startEarly: (id: string, token: string) =>
    request<GameState>(`/api/games/${id}/start`, { token }) as Promise<GameState>,

  /** Watch a game you hold no seat at. Null when nothing changed since v. */
  watchGame: (id: string, v = 0) => request<GameState>(`/api/games/${id}/watch?v=${v}`),

  // Returns null when the server reports "unchanged" (204) for version v.
  pollGame: (id: string, token: string, v: number) =>
    request<GameState>(`/api/games/${id}?token=${encodeURIComponent(token)}&v=${v}`),
  getGame: (id: string, token: string) =>
    request<GameState>(`/api/games/${id}?token=${encodeURIComponent(token)}`) as Promise<GameState>,

  legalMoves: async (id: string, token: string, from: string) =>
    (await request<{ moves: MoveOption[] }>(
      `/api/games/${id}/moves?token=${encodeURIComponent(token)}&from=${encodeURIComponent(from)}`,
    ))!.moves,

  move: (id: string, token: string, from: string, to: string) =>
    request<GameState>(`/api/games/${id}/move`, { token, from, to }) as Promise<GameState>,
  roll: (id: string, token: string, seat?: Seat) =>
    request<RollResult>(`/api/games/${id}/roll`, { token, seat: seat ?? "" }) as Promise<RollResult>,
  resign: (id: string, token: string) =>
    request<GameState>(`/api/games/${id}/resign`, { token }) as Promise<GameState>,
};

export function hasPath(mv: MoveRecord | null): mv is MoveRecord {
  return !!mv && Array.isArray(mv.path) && mv.path.length > 1;
}
