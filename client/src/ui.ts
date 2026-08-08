import type {
  BoardDto, CatalogItem, GameState, Lobby, Profile, RankEntry, RunningTable, Seat, Standings,
  TauntOption,
} from "./api";
import { RULES, drawDiagram } from "./rules";
import { RELEASES, VERSION } from "./changelog";
import type { Credit } from "./credits";
import { ASSET_CREDITS, creditLine } from "./credits";
import { countryList, countryName, flagChip } from "./flags";
import type { Track } from "./music";
import { seatInfo, seatName } from "./seats";

// Thin wrapper around the HTML overlay in index.html. The 3D scene owns the
// canvas; everything with text lives in the DOM (crisp, accessible, cheap).

const $ = <T extends HTMLElement>(id: string): T => document.getElementById(id) as T;

// Preview colors per skin, mirroring skins.ts (west swatch / east swatch).
const SKIN_SWATCH: Record<string, [string, string]> = {
  clay: ["#34302C", "#C9884F"],
  royal: ["#C9A227", "#C4C9CE"],
  crystal: ["#5FE3D6", "#F080BE"],
  rune: ["#4A4A48", "#5C6250"],
};
// Board finishes, mirroring BOARDS in theme.ts.
const BOARD_SWATCH: Record<string, [string, string]> = {
  slate: ["#2A2624", "#1A1614"],
  walnut: ["#4A3325", "#2E1E14"],
  ivory: ["#CFC4AC", "#9C907A"],
  forest: ["#1F3A2C", "#132419"],
  ink: ["#141317", "#08080A"],
};
const ENV_SWATCH: Record<string, [string, string]> = {
  picnic: ["#5E8C4A", "#b82c37"],
  fair: ["#A63A3A", "#FFB870"],
  dust2: ["#C9A96A", "#7E6438"],
  store: ["#8A6542", "#FFD9A0"],
  cafe: ["#2E2A26", "#C8BFAE"],
};

export interface TableOptions {
  name: string;
  players: number;
  password: string;
}

export interface UiHandlers {
  steamLogin(): void;
  guest(name: string, country: string): void;
  switchPlayer(): void;
  setCountry(code: string): void;
  browse(): void;
  refresh(): void;
  joinTable(gameId: string, password: string): void;
  watchTable(gameId: string): void;
  createTable(opts: TableOptions): void;
  offline(players: number): void;
  leaderboard(): void;
  startEarly(): void;
  resume(): void;
  leave(): void;
  resign(): void;
  focus(): void;
  rollDie(): void;
  toggleMusic(): void;
  sendTaunt(id: string): void;
  equip(id: string, kind: CatalogItem["kind"]): void;
  rematch(): void;
}

/** Why a seat dropped out, for the seat list. */
const OUT_CAUSE: Record<string, string> = {
  "king-captured": "king taken",
  "no-legal-move": "walled in",
  resigned: "resigned",
};

export class Ui {
  private toastTimer = 0;
  private nowPlayingTimer = 0;
  private resignArmed = 0;
  private profile: Profile | null = null;
  private catalog: CatalogItem[] = [];

  private players = 2;
  /** The locked table the password prompt is for. */
  private pendingJoin = "";

  // Latest lists from the server, plus how the player wants them shown.
  private lobbies: Lobby[] = [];
  private running: RunningTable[] = [];
  // "all" by default: landing on the full room is the point of a browser, and
  // the open tables sort to the top anyway.
  private filter: "open" | "running" | "all" = "all";
  /** True while the create card is being used to set up an offline game. */
  private offlineSetup = false;
  private hasTaunts = false;
  private boardDto: BoardDto | null = null;
  private sortKey: "name" | "players" | "age" = "players";
  private sortDesc = false;

  constructor(private readonly h: UiHandlers) {
    for (const btn of Array.from($("player-count").querySelectorAll("button"))) {
      btn.onclick = () => {
        this.players = Number(btn.dataset.players ?? 2);
        for (const other of Array.from($("player-count").querySelectorAll("button"))) {
          other.classList.toggle("on", other === btn);
        }
      };
    }
    this.fillCountries();

    $("btn-steam").onclick = () => h.steamLogin();
    $("btn-guest").onclick = () => this.submitGuest();
    $("btn-switch").onclick = () => h.switchPlayer();
    $("btn-browse").onclick = () => h.browse();
    $("btn-close-browser").onclick = () => this.showMenu(this.canResume);
    this.wireBrowser();
    $("btn-create").onclick = () => this.showCreate();
    $("btn-create-2").onclick = () => this.showCreate();
    $("btn-cancel-create").onclick = () => this.showMenu(this.canResume);
    $("btn-open-table").onclick = () => this.submitTable();
    $("btn-joinpass").onclick = () => this.submitPassword();
    $("btn-joinpass-cancel").onclick = () => {
      this.pendingJoin = "";
      $("joinpass").classList.add("hidden");
    };
    $("btn-offline").onclick = () => this.showOfflineSetup();
    $("btn-leaderboard").onclick = () => h.leaderboard();
    $("btn-close-board").onclick = () => this.showMenu(this.canResume);
    $("btn-start").onclick = () => h.startEarly();
    $("btn-resume").onclick = () => h.resume();
    $("btn-cancel").onclick = () => h.leave();
    $("btn-leave").onclick = () => h.leave();
    $("btn-back").onclick = () => h.leave();
    $("btn-rematch").onclick = () => h.rematch();
    $("btn-focus").onclick = () => h.focus();
    $("btn-throw").onclick = () => h.rollDie();
    $("btn-resign").onclick = () => this.onResign();
    $("btn-cancel-flag").onclick = () => $("flagpicker").classList.add("hidden");
    $("btn-save-flag").onclick = () => {
      $("flagpicker").classList.add("hidden");
      h.setCountry($<HTMLSelectElement>("my-country").value);
    };
    $("btn-rules").onclick = () => this.showRules();
    $("btn-close-rules").onclick = () => this.showMenu(this.canResume);
    $("btn-version").textContent = `v${VERSION}`;
    $("btn-version").onclick = () => this.showChangelog();
    $("btn-close-changelog").onclick = () => $("changelog").classList.add("hidden");
    $("btn-taunt").onclick = () => this.toggleTauntMenu();
    // Anywhere else closes it, the way a menu should.
    document.addEventListener("pointerdown", (e) => {
      const bar = $("tauntbar");
      if (!bar.contains(e.target as Node)) $("taunt-menu").classList.add("hidden");
    });
    $("btn-music").onclick = () => h.toggleMusic();
    $("btn-collection").onclick = () => this.showCollection();
    $("btn-close-collection").onclick = () => $("collection").classList.add("hidden");

    onEnter($<HTMLInputElement>("guest-name"), () => this.submitGuest());
    onEnter($<HTMLInputElement>("table-name"), () => this.submitTable());
    onEnter($<HTMLInputElement>("table-pass"), () => this.submitTable());
    onEnter($<HTMLInputElement>("joinpass-input"), () => this.submitPassword());
  }

  private canResume = false;

  /** Search box, filter buttons, sortable headers and the refresh button. */
  private wireBrowser(): void {
    const search = $<HTMLInputElement>("lobby-search");
    // Redraw from the lists we already hold: filtering must feel instant and
    // must not wait on (or fire) a request.
    search.addEventListener("input", () => this.drawLobbies());
    search.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        search.value = "";
        this.drawLobbies();
      }
    });

    for (const btn of Array.from($("lobby-filter").querySelectorAll("button"))) {
      btn.onclick = () => {
        this.filter = (btn.dataset.filter as typeof this.filter) ?? "open";
        for (const other of Array.from($("lobby-filter").querySelectorAll("button"))) {
          other.classList.toggle("on", other === btn);
        }
        this.drawLobbies();
      };
    }

    for (const th of Array.from(document.querySelectorAll<HTMLElement>("th.sortable"))) {
      th.onclick = () => {
        const key = (th.dataset.sort as typeof this.sortKey) ?? "name";
        if (this.sortKey === key) this.sortDesc = !this.sortDesc;
        else {
          this.sortKey = key;
          this.sortDesc = false;
        }
        this.drawLobbies();
      };
    }

    const refresh = $("btn-refresh");
    refresh.onclick = () => {
      refresh.classList.remove("spin");
      void refresh.offsetWidth; // restart the animation on a repeat click
      refresh.classList.add("spin");
      this.h.refresh();
    };
  }

  private fillCountries(): void {
    for (const id of ["guest-country", "my-country"]) {
      const select = $<HTMLSelectElement>(id);
      select.add(new Option("Country…", ""));
      for (const c of countryList()) select.add(new Option(c.name, c.code));
    }
  }

  /**
   * Preselects the country guessed from the player's address. Only ever a
   * default: it is shown in the open picker, so nobody gets a flag they did
   * not see. A code we have no flag for is left alone.
   */
  suggestCountry(code: string): void {
    const select = $<HTMLSelectElement>("guest-country");
    if (select.value) return; // the player already chose
    const wanted = code.toUpperCase();
    if ([...select.options].some((o) => o.value === wanted)) select.value = wanted;
  }

  private submitGuest(): void {
    const name = $<HTMLInputElement>("guest-name").value.trim();
    if (!name) {
      this.toast("Give the herald a name to announce.");
      return;
    }
    this.h.guest(name, $<HTMLSelectElement>("guest-country").value);
  }

  private submitTable(): void {
    if (this.offlineSetup) {
      this.h.offline(this.players);
      return;
    }
    this.h.createTable({
      name: $<HTMLInputElement>("table-name").value.trim(),
      players: this.players,
      password: $<HTMLInputElement>("table-pass").value,
    });
  }

  private submitPassword(): void {
    if (!this.pendingJoin) return;
    this.h.joinTable(this.pendingJoin, $<HTMLInputElement>("joinpass-input").value);
  }

  ready(): void {
    $("loading").classList.add("done");
    setTimeout(() => $("loading").classList.add("hidden"), 450);
  }

  loadingText(msg: string): void {
    $("loading-text").textContent = msg;
  }

  setData(profile: Profile | null, catalog: CatalogItem[]): void {
    this.profile = profile;
    this.catalog = catalog;
    this.refreshStats();
    this.renderMe();
    if (!$("collection").classList.contains("hidden")) this.renderCollection();
  }

  /**
   * Reflects the music state: the button, and a "now playing" line that shows
   * itself for a few seconds when the track changes and then fades out again.
   */
  setMusic(muted: boolean, track: Track | null, playlist: readonly Track[]): void {
    const btn = $("btn-music");
    btn.textContent = muted ? "♪̷" : "♪";
    btn.title = muted ? "Music off — click to play" : "Music on — click to mute";
    btn.classList.toggle("ghost", muted);

    const line = $("nowplaying");
    const label = track ? `${track.title} — ${track.artist}` : "";
    if (muted || !label) {
      line.classList.add("faded");
    } else if (line.textContent !== label) {
      line.textContent = label;
      line.classList.remove("faded");
      clearTimeout(this.nowPlayingTimer);
      this.nowPlayingTimer = window.setTimeout(() => line.classList.add("faded"), 6000);
    }

    this.renderCredits(playlist);
  }

  /**
   * Everything borrowed, credited. All of it is CC-BY, which requires the
   * attribution to reach the people using the work — so it is rendered from
   * the asset data and cannot drift out of sync with what actually ships.
   */
  private renderCredits(playlist: readonly Track[]): void {
    const tracks: Credit[] = playlist.map((t) => ({
      title: t.title,
      author: t.artist,
      licence: t.licence,
      source: t.source,
    }));
    $("credits").replaceChildren(...[...tracks, ...ASSET_CREDITS].map(creditLine));
  }

  /** Grey out Steam sign-in where the server has no realm configured. */
  setSteamAvailable(available: boolean): void {
    const btn = $<HTMLButtonElement>("btn-steam");
    btn.disabled = !available;
    if (!available) {
      $("steam-hint").textContent = "Steam sign-in is not configured on this server.";
    }
  }

  private refreshStats(): void {
    const p = this.profile;
    if (!p) {
      $("menu-stats").textContent = "";
      $("collection-stats").textContent = "";
      return;
    }
    const text = `Battles fought: ${p.gamesPlayed}  ·  Victories: ${p.wins}`;
    $("menu-stats").textContent = p.persistent
      ? text
      : `${text}  ·  guest — nothing is kept when you leave`;
    $("collection-stats").textContent = p.persistent
      ? `${text}  ·  progress comes from online battles`
      : `${text}  ·  sign in through Steam to keep what you unlock`;
  }

  /** The identity strip on the menu: avatar, name, flag. */
  private renderMe(): void {
    const host = $("me");
    host.replaceChildren();
    const p = this.profile;
    if (!p) return;
    if (p.avatar) {
      const img = document.createElement("img");
      img.src = p.avatar;
      img.alt = "";
      img.onerror = () => img.remove();
      host.appendChild(img);
    }
    const nick = document.createElement("span");
    nick.className = "nick";
    nick.textContent = p.name || "Wanderer";
    host.appendChild(nick);

    // The flag is a guess, so it has to be correctable — and a signed-in
    // player never passes the sign-in screen where the picker used to live.
    if (p.country) {
      const chip = flagChip(p.country);
      chip.title = `${countryName(p.country)} — click to change`;
      chip.onclick = () => this.showFlagPicker();
      host.appendChild(chip);
    } else {
      const set = document.createElement("button");
      set.className = "setflag";
      set.textContent = "set your flag";
      set.onclick = () => this.showFlagPicker();
      host.appendChild(set);
    }
  }

  /**
   * How to play. The board arrives after the rules screen is wired up, so it
   * is handed in later; without it the words still stand on their own.
   */
  setBoard(board: BoardDto): void {
    this.boardDto = board;
  }

  private showRules(): void {
    this.hideAll();
    const list = $("rules-list");
    list.replaceChildren();
    for (const rule of RULES) {
      const box = document.createElement("div");
      box.className = "rule";

      const words = document.createElement("div");
      words.className = "words";
      const h = document.createElement("h2");
      h.textContent = rule.title;
      words.appendChild(h);
      for (const line of rule.body) {
        const p = document.createElement("p");
        p.textContent = line;
        words.appendChild(p);
      }
      box.appendChild(words);

      if (rule.diagram && this.boardDto) {
        box.appendChild(drawDiagram(this.boardDto, rule.diagram, 220));
      }
      list.appendChild(box);
    }
    $("rules").classList.remove("hidden");
    document.body.classList.add("hud-hidden");
  }

  /** The version, and behind it what each one changed. */
  private showChangelog(): void {
    $("changelog-version").textContent = `You are playing version ${VERSION}.`;
    const list = $("changelog-list");
    list.replaceChildren();
    RELEASES.forEach((rel, i) => {
      const box = document.createElement("div");
      box.className = "release" + (i > 0 ? " old" : "");

      const head = document.createElement("div");
      const ver = document.createElement("span");
      ver.className = "ver";
      ver.textContent = `v${rel.version}`;
      const when = document.createElement("span");
      when.className = "when";
      when.textContent = new Date(rel.date + "T00:00:00").toLocaleDateString(undefined, {
        year: "numeric",
        month: "long",
        day: "numeric",
      });
      head.append(ver, when);
      box.appendChild(head);

      if (rel.headline) {
        const line = document.createElement("div");
        line.className = "headline";
        line.textContent = rel.headline;
        box.appendChild(line);
      }

      const ul = document.createElement("ul");
      for (const change of rel.changes) {
        const li = document.createElement("li");
        li.textContent = change;
        ul.appendChild(li);
      }
      box.appendChild(ul);
      list.appendChild(box);
    });
    $("changelog").classList.remove("hidden");
  }

  private showFlagPicker(): void {
    const select = $<HTMLSelectElement>("my-country");
    select.value = this.profile?.country ?? "";
    $("flagpicker").classList.remove("hidden");
  }

  // ---- screens ----

  private hideAll(): void {
    for (const id of [
      "signin", "menu", "browser", "create", "joinpass", "lobby", "result",
      "collection", "leaderboard", "flagpicker", "changelog", "rules",
    ]) {
      $(id).classList.add("hidden");
    }
  }

  showSignIn(): void {
    this.hideAll();
    $("signin").classList.remove("hidden");
    document.body.classList.add("hud-hidden");
  }

  showMenu(canResume: boolean): void {
    this.canResume = canResume;
    this.hideAll();
    $("menu").classList.remove("hidden");
    document.body.classList.add("hud-hidden");
    $("btn-resume").classList.toggle("hidden", !canResume);
    this.refreshStats();
    this.renderMe();
  }

  showBrowser(): void {
    this.hideAll();
    $("browser").classList.remove("hidden");
    document.body.classList.add("hud-hidden");
  }

  browsing(): boolean {
    return !$("browser").classList.contains("hidden");
  }

  /**
   * The hall of champions. Below the threshold you are simply absent — not
   * ranked low, not greyed out — which is what makes arriving on it mean
   * something. So an unqualified player gets told the distance instead.
   */
  showStandings(board: Standings): void {
    this.hideAll();
    $("leaderboard").classList.remove("hidden");
    document.body.classList.add("hud-hidden");

    const note = $("board-note");
    if (board.winsNeeded > 0) {
      note.textContent =
        board.winsNeeded === 1
          ? `${board.minWins} victories earn a place. One more and you are on it.`
          : `${board.minWins} victories earn a place — ${board.winsNeeded} to go.`;
    } else if (board.you?.rank) {
      note.textContent = `You stand at number ${board.you.rank}.`;
    } else if (this.profile && !this.profile.persistent) {
      note.textContent = `${board.minWins} victories earn a place. Sign in through Steam to be counted.`;
    } else {
      note.textContent = `${board.minWins} victories earn a place.`;
    }

    const body = $("board-rows");
    body.replaceChildren();
    for (const e of board.entries) body.appendChild(rankRow(e));
    if (!board.entries.length) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 4;
      td.className = "empty";
      td.textContent = "Nobody has reached it yet. The first place is open.";
      tr.appendChild(td);
      body.appendChild(tr);
    }
  }

  private showCreate(): void {
    this.offlineSetup = false;
    this.hideAll();
    $("create").classList.remove("hidden");
    $("create-title").textContent = "Open a table";
    $("create-note").classList.add("hidden");
    $("row-table-name").classList.remove("hidden");
    $("row-table-pass").classList.remove("hidden");
    $("btn-open-table").textContent = "Open the table";
    const name = $<HTMLInputElement>("table-name");
    if (!name.value) name.placeholder = `${this.profile?.name ?? "Your"}'s table`;
    $<HTMLInputElement>("table-pass").value = "";
  }

  /**
   * Offline reuses the same card: the only thing a shared-screen game needs is
   * how many people are round it. No name, no password, nobody to invite.
   */
  private showOfflineSetup(): void {
    this.showCreate();
    this.offlineSetup = true;
    $("create-title").textContent = "Offline game";
    const note = $("create-note");
    note.textContent =
      "Everyone plays on this screen, taking turns. Nothing is recorded — no victories, no unlocks.";
    note.classList.remove("hidden");
    $("row-table-name").classList.add("hidden");
    $("row-table-pass").classList.add("hidden");
    $("btn-open-table").textContent = "Set up the board";
  }

  /**
   * Takes the latest lists and redraws the table under the current search,
   * filter and sort. Held in a field so typing in the search box or clicking a
   * column can redraw without waiting for the next poll.
   */
  renderLobbies(lobbies: Lobby[], running: RunningTable[]): void {
    this.lobbies = lobbies;
    this.running = running;
    this.drawLobbies();
  }

  private drawLobbies(): void {
    const q = $<HTMLInputElement>("lobby-search").value.trim().toLowerCase();
    const matches = (name: string, host: string, country: string) =>
      !q ||
      name.toLowerCase().includes(q) ||
      host.toLowerCase().includes(q) ||
      country.toLowerCase().includes(q) ||
      countryName(country).toLowerCase().includes(q);

    const open = this.filter === "running" ? [] : this.lobbies.filter((l) => matches(l.name, l.host, l.country));
    const live = this.filter === "open" ? [] : this.running.filter((r) => matches(r.name, r.host, r.country));

    const dir = this.sortDesc ? -1 : 1;
    open.sort((a, b) => dir * this.compareOpen(a, b));
    live.sort((a, b) => dir * this.compareRunning(a, b));

    const body = $("lobby-rows");
    body.replaceChildren();
    for (const l of open) body.appendChild(this.lobbyRow(l));
    for (const r of live) {
      body.appendChild(
        runningRow(r, (id) => this.h.watchTable(id), (id) => this.h.joinTable(id, "")),
      );
    }

    if (!open.length && !live.length) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 4;
      td.className = "empty";
      td.textContent = q
        ? `Nothing matches “${$<HTMLInputElement>("lobby-search").value.trim()}”.`
        : this.filter === "running"
          ? "Nothing under way at the moment."
          : "No open tables right now — open one yourself.";
      tr.appendChild(td);
      body.appendChild(tr);
    }

    const total = open.length + live.length;
    $("lobby-count").textContent = `${total} ${total === 1 ? "table" : "tables"}`;
    for (const th of Array.from(document.querySelectorAll<HTMLElement>("th.sortable"))) {
      const on = th.dataset.sort === this.sortKey;
      th.classList.toggle("on", on);
      th.querySelector(".arrow")!.textContent = on ? (this.sortDesc ? "▼" : "▲") : "";
    }
  }

  private compareOpen(a: Lobby, b: Lobby): number {
    switch (this.sortKey) {
      case "players":
        // Nearly-full tables first: those are the ones about to start.
        return b.taken / b.players - a.taken / a.players || a.name.localeCompare(b.name);
      case "age":
        return a.age - b.age;
      default:
        return a.name.localeCompare(b.name);
    }
  }

  private compareRunning(a: RunningTable, b: RunningTable): number {
    switch (this.sortKey) {
      case "players":
        return b.players - a.players || a.name.localeCompare(b.name);
      case "age":
        return a.minutes - b.minutes;
      default:
        return a.name.localeCompare(b.name);
    }
  }

  private lobbyRow(l: Lobby): HTMLElement {
    const row = document.createElement("tr");
    if (l.yours) row.classList.add("mine");

    const name = document.createElement("td");
    const box = document.createElement("div");
    box.className = "tname";
    box.appendChild(flagChip(l.country));
    const title = document.createElement("span");
    title.className = "name";
    title.textContent = l.name || "A table";
    title.title = l.name;
    box.appendChild(title);
    if (l.locked) {
      const lock = document.createElement("span");
      lock.className = "lock";
      lock.textContent = "🔒";
      lock.title = "Password required";
      box.appendChild(lock);
    }
    const host = document.createElement("span");
    host.className = "host";
    host.textContent = l.host || "someone";
    box.appendChild(host);
    name.appendChild(box);

    const seats = document.createElement("td");
    seats.className = "num";
    const count = document.createElement("span");
    count.className = "seats";
    count.textContent = `${l.taken} / ${l.players}`;
    seats.appendChild(count);

    const when = document.createElement("td");
    when.className = "when";
    when.textContent = ago(l.age);

    const act = document.createElement("td");
    act.className = "act";
    const btn = document.createElement("button");
    btn.className = "primary";
    btn.textContent = l.yours ? "Return" : "Join";
    btn.onclick = () => {
      if (l.locked && !l.yours) {
        this.askPassword(l);
        return;
      }
      this.h.joinTable(l.gameId, "");
    };
    act.appendChild(btn);

    row.append(name, seats, when, act);
    return row;
  }

  private askPassword(l: Lobby): void {
    this.pendingJoin = l.gameId;
    $("joinpass-name").textContent = `“${l.name}” — ${l.host} keeps this one closed.`;
    const input = $<HTMLInputElement>("joinpass-input");
    input.value = "";
    $("joinpass").classList.remove("hidden");
    input.focus();
  }

  /** Back to the browser after a wrong password, keeping the prompt open. */
  passwordRejected(msg: string): void {
    this.toast(msg);
    $<HTMLInputElement>("joinpass-input").select();
  }

  showLobby(state: GameState): void {
    this.hideAll();
    $("lobby").classList.remove("hidden");
    document.body.classList.add("hud-hidden");

    const free = state.seats.filter((s) => !s.taken).length;
    const mySeat = state.seats.find((s) => s.you);
    $("lobbyname").textContent = state.name || "Your table";
    $("lobbywait").textContent =
      free === 1 ? "Awaiting one more player…" : `Awaiting ${free} more players…`;

    const host = $("lobbyseats");
    host.replaceChildren();
    for (const seat of state.seats) {
      const info = seatInfo(seat.seat);
      const line = document.createElement("div");
      line.className = "seatline" + (seat.taken ? "" : " open");
      line.style.borderLeftColor = info.css;
      if (seat.taken && seat.country) line.appendChild(flagChip(seat.country));
      const who = document.createElement("span");
      who.className = "who";
      who.textContent = seat.taken
        ? `${seat.name || info.colourName}${seat.you ? " (you)" : ""}`
        : "open seat";
      line.appendChild(who);
      host.appendChild(line);
    }
    // Anyone seated may start a table that is not filling up — on a table the
    // house opened, the first seat belongs to the computer, so tying this to
    // the host would strand the one real player there for ever.
    $("btn-start").classList.toggle("hidden", !(mySeat && free > 0));
  }

  showGame(): void {
    this.hideAll();
    document.body.classList.remove("hud-hidden");
    document.body.classList.remove("watching");
    this.disarmResign();
  }

  /**
   * Watching somebody else's game. Same board, but the controls that only make
   * sense with a seat are gone — a spectator with a Resign button is a bug
   * waiting to be reported.
   */
  showWatching(): void {
    this.hideAll();
    document.body.classList.remove("hud-hidden");
    document.body.classList.add("watching");
  }

  showResult(title: string, reason: string): void {
    $("result-title").textContent = title;
    $("result-reason").textContent = reason;
    $("btn-rematch").classList.remove("hidden");
    ($("btn-rematch") as HTMLButtonElement).disabled = false;
    $("btn-rematch").textContent = "Rematch";
    $("rematch-note").classList.add("hidden");
    $("result").classList.remove("hidden");
  }

  /** Somebody at the table has asked for another game; say so. */
  rematchOffered(): void {
    const btn = $("btn-rematch");
    btn.textContent = "Join the rematch";
    btn.classList.add("primary");
    const note = $("rematch-note");
    note.textContent = "Your opponent wants another game.";
    note.classList.remove("hidden");
  }

  /** We asked first and are waiting for the others to follow. */
  rematchWaiting(): void {
    ($("btn-rematch") as HTMLButtonElement).disabled = true;
    $("btn-rematch").textContent = "Waiting…";
    const note = $("rematch-note");
    note.textContent = "Waiting for the other player to accept.";
    note.classList.remove("hidden");
  }

  /**
   * The move clock. Rendered from the deadline rather than a countdown we hold
   * ourselves, so a slow poll cannot make the numbers jump — and shown only
   * while it is actually your move, because a clock ticking on somebody else's
   * turn just makes people anxious.
   */
  setClock(deadline: string, yours: boolean): void {
    const el = $("clock");
    if (!deadline || !yours) {
      el.classList.add("hidden");
      return;
    }
    const left = Math.max(0, Math.round((Date.parse(deadline) - Date.now()) / 1000));
    el.classList.remove("hidden");
    el.textContent = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`;
    el.classList.toggle("urgent", left <= 30);
    el.classList.toggle("blink", left <= 10);
    el.title = "Move before this runs out, or you forfeit the game";
  }

  updateFromState(state: GameState, myTurn: boolean): void {
    if (state.status === "waiting") {
      this.showLobby(state);
      return;
    }
    this.showGame();
    const rolling = state.status === "active" && state.phase === "roll";
    ($("btn-throw") as HTMLButtonElement).classList.toggle("hidden", !(rolling && state.yourRoll));
    if (rolling) {
      const waiting = (state.pending ?? []).map((s) => seatName(s));
      this.status(
        state.yourRoll
          ? `Roll for the opening move — click the die (${waiting[0] ?? ""})`
          : `Waiting for ${waiting.join(", ")} to roll…`,
      );
    } else if (state.status === "active") {
      const opening = !state.capturing ? " · opening round, no strikes" : "";
      this.status(
        state.you === "all"
          ? `${seatName(state.turn)} to move — drag a stone${opening}`
          : myTurn
            ? `⚔ Your move — drag a stone${opening}`
            : `${seatName(state.turn)} is thinking…${opening}`,
      );
    }
    $("seat").textContent =
      state.you === "all"
        ? `Offline — all ${state.players} seats on this screen`
        : `You are ${seatName(state.you as never)}`;
    $("gamecode").textContent = state.mode === "online" ? state.name : "";
    ($("btn-resign") as HTMLButtonElement).disabled = state.status !== "active";
    this.renderPlayers(state);
  }

  /** The same HUD from a spectator's chair: the state of play, no controls. */
  updateWhileWatching(state: GameState): void {
    this.showWatching();
    if (state.status === "finished") {
      this.status(
        state.winner ? `${seatName(state.winner)} won this one` : "That game ended in a draw",
      );
    } else if (state.phase === "roll") {
      this.status(`Rolling for the opening move — ${(state.pending ?? []).map(seatName).join(", ")}`);
    } else {
      this.status(`${seatName(state.turn)} to move — move ${state.ply}`);
    }
    $("seat").textContent = "Watching";
    $("gamecode").textContent = state.name;
    this.renderPlayers(state);
  }

  // ---- taunts ----

  /** Fills the menu from the server's catalogue. */
  setTaunts(options: readonly TauntOption[]): void {
    const menu = $("taunt-menu");
    menu.replaceChildren();
    for (const o of options) {
      const btn = document.createElement("button");
      btn.textContent = o.text;
      btn.dataset.taunt = o.id;
      btn.onclick = () => {
        menu.classList.add("hidden");
        this.h.sendTaunt(o.id);
      };
      menu.appendChild(btn);
    }
    this.hasTaunts = options.length > 0;
  }

  /** Whether the taunt button is offered at all, and whether it is ready. */
  setTauntState(available: boolean, cooldownLeft: number): void {
    $("tauntbar").classList.toggle("hidden", !(available && this.hasTaunts));
    const btn = $<HTMLButtonElement>("btn-taunt");
    btn.disabled = cooldownLeft > 0;
    btn.title = cooldownLeft > 0 ? "Give it a moment" : "Say something";
    if (cooldownLeft > 0) $("taunt-menu").classList.add("hidden");
  }

  private toggleTauntMenu(): void {
    $("taunt-menu").classList.toggle("hidden");
  }

  /**
   * Shows what somebody said. The bubble carries the words even when the sound
   * is off or blocked, which is the whole reason the text exists.
   */
  showTaunt(seat: Seat, name: string, text: string): void {
    const box = document.createElement("div");
    box.className = "bubble";
    box.style.borderLeftColor = seatInfo(seat).css;

    const who = document.createElement("div");
    who.className = "who";
    who.textContent = name || seatName(seat);
    const said = document.createElement("div");
    said.className = "said";
    said.textContent = text;
    box.append(who, said);

    const host = $("bubbles");
    host.appendChild(box);
    while (host.childElementCount > 4) host.firstElementChild?.remove();
    setTimeout(() => {
      box.classList.add("going");
      setTimeout(() => box.remove(), 600);
    }, 5200);
  }

  /** The seat list: colour, who is on the move, and who is out (and why). */
  private renderPlayers(state: GameState): void {
    const host = $("players");
    host.replaceChildren();
    const lost = new Map<string, number>();
    for (const p of state.pieces) {
      if (p.captured) lost.set(p.owner, (lost.get(p.owner) ?? 0) + 1);
    }

    for (const seat of state.seats) {
      const info = seatInfo(seat.seat);
      const row = document.createElement("div");
      row.className = "player";
      row.style.borderLeftColor = info.css;
      if (state.status === "active" && state.turn === seat.seat) row.classList.add("turn");
      if (seat.out) row.classList.add("gone");
      if (seat.you && state.you !== "all") row.classList.add("mine");

      const dot = document.createElement("div");
      dot.className = "dot";
      dot.style.background = info.css;
      row.append(dot);
      // A signed-in player's Steam avatar, where they have one.
      if (seat.taken && seat.avatar) {
        const img = document.createElement("img");
        img.className = "face";
        img.src = seat.avatar;
        img.alt = "";
        img.onerror = () => img.remove(); // a dead avatar URL must not leave a gap
        row.append(img);
      }
      if (seat.taken && seat.country) row.append(flagChip(seat.country));

      const who = document.createElement("div");
      who.className = "who";
      // Real names once we have them; the seat's colour is the fallback.
      who.textContent = !seat.taken
        ? `${info.label} · open seat`
        : seat.name
          ? `${seat.name} · ${info.label}`
          : `${info.label} · ${info.colourName}`;
      // Worth knowing which computer you are up against.
      if (seat.bot && seat.level) who.title = `Computer player · ${seat.level}`;

      const tag = document.createElement("div");
      tag.className = seat.out ? "tag" : "lost";
      const throwing = state.phase === "roll" && (state.pending ?? []).includes(seat.seat);
      if (seat.out) tag.textContent = OUT_CAUSE[seat.cause ?? ""] ?? "out";
      else if (throwing) {
        tag.className = "tag";
        tag.textContent = "to roll";
        row.classList.add("turn");
      } else if (state.phase === "play" && state.status === "active" && state.turn === seat.seat) {
        tag.className = "tag";
        tag.textContent = "to move";
      } else {
        const n = lost.get(seat.seat) ?? 0;
        tag.textContent = n ? `−${n}` : "";
      }

      row.append(who, tag);
      host.appendChild(row);
    }
  }

  status(text: string): void {
    $("status").textContent = text;
  }

  toast(msg: string, good = false): void {
    const el = $("toast");
    el.textContent = msg;
    el.classList.toggle("good", good);
    el.classList.add("show");
    clearTimeout(this.toastTimer);
    this.toastTimer = window.setTimeout(() => el.classList.remove("show"), 3400);
  }

  // Two-step resign confirmation without a modal.
  private onResign(): void {
    const btn = $("btn-resign");
    if (Date.now() < this.resignArmed) {
      this.disarmResign();
      this.h.resign();
      return;
    }
    this.resignArmed = Date.now() + 3000;
    btn.textContent = "Sure?";
    setTimeout(() => {
      if (Date.now() >= this.resignArmed) this.disarmResign();
    }, 3100);
  }

  private disarmResign(): void {
    this.resignArmed = 0;
    $("btn-resign").textContent = "Resign";
  }

  // ---- collection ----

  private showCollection(): void {
    this.renderCollection();
    $("collection").classList.remove("hidden");
  }

  private renderCollection(): void {
    const list = $("collection-list");
    list.replaceChildren();
    const p = this.profile;
    this.refreshStats();

    const section = (label: string, kind: CatalogItem["kind"]) => {
      const head = document.createElement("div");
      head.className = "section";
      head.textContent = label;
      list.appendChild(head);
      for (const item of this.catalog.filter((i) => i.kind === kind)) {
        list.appendChild(this.itemRow(item, p));
      }
    };
    section("PIECE SKINS", "skin");
    section("BOARDS", "board");
    section("ENVIRONMENTS", "env");
  }

  private itemRow(item: CatalogItem, profile: Profile | null): HTMLElement {
    const unlocked = !!profile?.unlocked.includes(item.id);
    const equipped =
      !!profile &&
      ((item.kind === "skin" && profile.equippedSkin === item.id) ||
        (item.kind === "env" && profile.equippedEnv === item.id) ||
        (item.kind === "board" && profile.equippedBoard === item.id));

    const row = document.createElement("div");
    row.className = "item" + (unlocked ? "" : " locked");

    const swatches =
      item.kind === "skin" ? SKIN_SWATCH : item.kind === "board" ? BOARD_SWATCH : ENV_SWATCH;
    const colors = swatches[item.id] ?? ["#555", "#888"];
    const swatch = document.createElement("div");
    swatch.className = "swatch";
    swatch.style.background = `linear-gradient(135deg, ${colors[0]} 0 50%, ${colors[1]} 50% 100%)`;
    row.appendChild(swatch);

    const info = document.createElement("div");
    info.className = "info";
    const name = document.createElement("div");
    name.className = "name";
    name.textContent = item.name;
    const desc = document.createElement("div");
    desc.className = "desc";
    desc.textContent = unlocked
      ? item.desc
      : item.needWins > 0
        ? `Locked — win ${item.needWins} ${item.needWins === 1 ? "battle" : "battles"}`
        : `Locked — fight ${item.needPlays} battles`;
    info.append(name, desc);
    row.appendChild(info);

    const btn = document.createElement("button");
    btn.textContent = equipped ? "Equipped" : unlocked ? "Equip" : "Locked";
    if (equipped) btn.classList.add("primary");
    btn.disabled = !unlocked || equipped;
    btn.onclick = () => this.h.equip(item.id, item.kind);
    row.appendChild(btn);
    return row;
  }
}

// ---- small builders ----

function onEnter(input: HTMLInputElement, run: () => void): void {
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") run();
  });
}

/** A game under way: not joinable, but you can pull up a chair and watch. */
function runningRow(
  r: RunningTable,
  onWatch: (id: string) => void,
  onReturn: (id: string) => void,
): HTMLElement {
  const row = document.createElement("tr");
  row.className = r.yours ? "mine" : "running";

  const name = document.createElement("td");
  const box = document.createElement("div");
  box.className = "tname";
  box.appendChild(flagChip(r.country));
  const title = document.createElement("span");
  title.className = "name";
  title.textContent = r.name || "A game";
  title.title = r.name;
  const host = document.createElement("span");
  host.className = "host";
  host.textContent = r.host;
  box.append(title, host);
  name.appendChild(box);

  const seats = document.createElement("td");
  seats.className = "num";
  const count = document.createElement("span");
  count.className = "seats full";
  count.textContent = `${r.players} / ${r.players}`;
  seats.appendChild(count);

  const when = document.createElement("td");
  when.className = "when";
  when.textContent = r.ply < 2 ? `just started · ${r.minutes} min` : `move ${r.ply} · ${r.minutes} min`;

  const act = document.createElement("td");
  act.className = "act";
  const btn = document.createElement("button");
  // Your own game, still going: offer the chair back rather than a seat in
  // the audience for a match you are supposed to be playing.
  btn.className = r.yours ? "primary" : "ghost";
  btn.textContent = r.yours ? "Return" : "Watch";
  btn.onclick = () => (r.yours ? onReturn(r.gameId) : onWatch(r.gameId));
  act.appendChild(btn);

  row.append(name, seats, when, act);
  return row;
}

/** One line of the hall of champions. */
function rankRow(e: RankEntry): HTMLElement {
  const row = document.createElement("tr");
  if (e.you) row.classList.add("mine");

  const rank = document.createElement("td");
  rank.className = "num seats";
  rank.textContent = String(e.rank ?? "");

  const who = document.createElement("td");
  const box = document.createElement("div");
  box.className = "tname";
  if (e.avatar) {
    const img = document.createElement("img");
    img.className = "face";
    img.src = e.avatar;
    img.alt = "";
    img.onerror = () => img.remove();
    box.appendChild(img);
  }
  if (e.country) box.appendChild(flagChip(e.country));
  const name = document.createElement("span");
  name.className = "name";
  name.textContent = e.name || "A player";
  box.appendChild(name);
  if (e.you) {
    const tag = document.createElement("span");
    tag.className = "host";
    tag.textContent = "you";
    box.appendChild(tag);
  }
  who.appendChild(box);

  const wins = document.createElement("td");
  wins.className = "num seats";
  wins.textContent = String(e.wins);

  const played = document.createElement("td");
  played.className = "num when";
  played.textContent = String(e.played);

  row.append(rank, who, wins, played);
  return row;
}

/** "just now" / "4 min" — the age of an open table. */
function ago(seconds: number): string {
  if (seconds < 45) return "just opened";
  const min = Math.round(seconds / 60);
  if (min < 60) return `waiting ${min} min`;
  return `waiting ${Math.round(min / 60)} h`;
}
