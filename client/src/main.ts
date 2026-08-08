import { ArcRotateCamera, Engine, GlowLayer, Matrix, Plane, PointerEventTypes, Scene, Vector3 } from "./babylon";
import type { Mesh, PointerInfo, ShadowGenerator } from "./babylon";
import { ApiError, api, hasPath } from "./api";
import type { BoardDto, CatalogItem, GameState, MoveOption, Profile, Seat } from "./api";
import { BoardView } from "./board";
import { DiceRoller } from "./dice";
import { buildEnvironment } from "./environments";
import type { BuiltEnvironment } from "./environments";
import { Music } from "./music";
import { Taunts } from "./taunts";
import { seatInfo, seatName } from "./seats";
import { Ui } from "./ui";

const STORE = { profile: "kr_profile", game: "kr_game", token: "kr_token" };
const POLL_MS = 1200;
const LOBBY_MS = 4000; // how often the board room refreshes itself
const DRAG_THRESHOLD = 6; // px before a press becomes a drag
const GROUND = Plane.FromPositionAndNormal(Vector3.Zero(), new Vector3(0, 1, 0));

const REASONS: Record<string, string> = {
  throne: "the King has reached the Gilded Throne",
  "last-standing": "every rival has fallen",
  "move-limit": "the long war exhausts every host — a draw",
};


class Kingsreach {
  private readonly engine: Engine;
  private readonly scene: Scene;
  private readonly camera: ArcRotateCamera;
  private readonly board: BoardView;
  private readonly diceRoller: DiceRoller;
  private readonly music = new Music();
  private readonly taunts = new Taunts();
  private readonly ui: Ui;

  private env: BuiltEnvironment | null = null;
  private envId = "";
  private shadows: ShadowGenerator | null = null;
  private readonly casters: Mesh[] = [];

  private profile: Profile | null = null;
  private catalog: CatalogItem[] = [];
  /** Kept so the board can be rebuilt when its finish changes. */
  private boardDto: BoardDto | null = null;
  private clockTimer = 0;
  private rematchOffered = false;
  /** True while watching somebody else's game: no seat, no interaction. */
  private watching = false;

  private state: GameState | null = null;
  private gameId = "";
  private token = "";
  private selected: string | null = null;
  private options: MoveOption[] = [];
  private busy = false;
  private animating = false;
  private pollTimer = 0;
  private lobbyTimer = 0;
  private applyQueue: Promise<void> = Promise.resolve();

  // pointer/drag bookkeeping
  private pressPiece: string | null = null;
  private pressX = 0;
  private pressY = 0;
  private dragging = false;
  private cameraDetached = false;

  constructor(canvas: HTMLCanvasElement) {
    this.engine = new Engine(canvas, true, { preserveDrawingBuffer: false, stencil: false }, true);
    this.scene = new Scene(this.engine);

    this.camera = new ArcRotateCamera("camera", Math.PI, 0.95, 15, new Vector3(0, 0.2, 0), this.scene);
    this.camera.lowerBetaLimit = 0.22;
    this.camera.upperBetaLimit = 1.45;
    this.camera.lowerRadiusLimit = 7;
    this.camera.upperRadiusLimit = 28;
    this.camera.wheelPrecision = 22;
    this.camera.panningSensibility = 0; // orbit + zoom only
    this.camera.attachControl(true);

    const glow = new GlowLayer("glow", this.scene);
    glow.intensity = 0.55;

    this.board = new BoardView(this.scene);
    this.board.glow = glow;
    this.board.onMeshes = (meshes) => this.registerCasters(meshes);

    this.diceRoller = new DiceRoller(this.scene);
    this.ui = new Ui({
      steamLogin: () => this.steamLogin(),
      guest: (name, country) => void this.signInAsGuest(name, country),
      switchPlayer: () => this.signOut(),
      setCountry: (code) => void this.setCountry(code),
      browse: () => void this.openBrowser(),
      refresh: () => void this.refreshLobbies(),
      joinTable: (id, password) => void this.joinGame(id, password),
      watchTable: (id) => void this.watchGame(id),
      createTable: (opts) => void this.createTable(opts),
      offline: (players) => void this.createGame({ mode: "offline", players }),
      leaderboard: () => void this.showLeaderboard(),
      startEarly: () => void this.startEarly(),
      resume: () => void this.resume(),
      leave: () => this.leaveToMenu(),
      resign: () => void this.resign(),
      focus: () => this.focusCamera(),
      rollDie: () => void this.rollDie(),
      toggleMusic: () => this.music.toggleMute(),
      sendTaunt: (id) => void this.sendTaunt(id),
      equip: (id, kind) => void this.equip(id, kind),
      rematch: () => void this.rematch(),
    });

    this.music.observe(() => this.ui.setMusic(this.music.isMuted, this.music.current, this.music.playlist));
    this.music.attachTo(window);

    this.scene.onPointerObservable.add((pi) => this.onPointer(pi));
    this.engine.runRenderLoop(() => this.scene.render());
    window.addEventListener("resize", () => this.engine.resize());
  }

  // ---- boot ----

  async start(): Promise<void> {
    this.ui.setMusic(this.music.isMuted, this.music.current, this.music.playlist);
    this.applyEnvironment("picnic");

    for (;;) {
      try {
        this.ui.loadingText("Unrolling the cloth…");
        this.boardDto = await api.board();
        this.board.build(this.boardDto);
        this.ui.setBoard(this.boardDto); // the rules diagrams draw from this
        break;
      } catch {
        this.ui.loadingText("Cannot reach the server — retrying…");
        await new Promise((r) => setTimeout(r, 2000));
      }
    }

    try {
      this.catalog = await api.catalog();
    } catch {
      this.catalog = [];
    }
    try {
      this.ui.setSteamAvailable((await api.config()).steam);
    } catch {
      this.ui.setSteamAvailable(false);
    }
    try {
      const { taunts, gapMs } = await api.tauntList();
      this.taunts.load(taunts, gapMs);
      this.ui.setTaunts(taunts);
    } catch {
      /* no taunts is a missing nicety, not a broken game */
    }
    // The button has a cooldown to show, so it needs its own gentle tick.
    setInterval(() => this.refreshTauntButton(), 1000);

    // A guess, not a fact — and never worth delaying the boot for.
    void api.geo().then((c) => c && this.ui.suggestCountry(c)).catch(() => {});

    await this.loadIdentity();
    if (this.profile) {
      this.applyEnvironment(this.profile.equippedEnv);
      this.applyBoardFinish(this.profile.equippedBoard);
    }
    this.ui.setData(this.profile, this.catalog);

    if (!this.profile) {
      this.ui.showSignIn();
    } else {
      this.ui.showMenu(await this.checkResumable());
    }
    this.ui.ready();
  }

  /**
   * Who is playing. A Steam sign-in comes back through the URL fragment; a
   * returning player is remembered in localStorage; anyone else is asked.
   */
  private async loadIdentity(): Promise<void> {
    const fromSteam = readSteamFragment();
    if (fromSteam === "failed") {
      this.ui.toast("Steam turned us away. Try again, or play as a guest.");
    } else if (fromSteam) {
      try {
        this.profile = await api.getProfile(fromSteam);
        localStorage.setItem(STORE.profile, fromSteam);
        this.ui.toast(`Welcome, ${this.profile.name}.`, true);
        return;
      } catch {
        this.ui.toast("That Steam session went stale.");
      }
    }

    const saved = localStorage.getItem(STORE.profile);
    if (!saved) return;
    try {
      this.profile = await api.getProfile(saved);
    } catch {
      localStorage.removeItem(STORE.profile); // the server forgot this one
    }
  }

  private steamLogin(): void {
    window.location.href = "/api/auth/steam/login";
  }

  private async signInAsGuest(name: string, country: string): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      this.profile = await api.createProfile(name, country);
      localStorage.setItem(STORE.profile, this.profile.token);
      this.ui.setData(this.profile, this.catalog);
      this.applyEnvironment(this.profile.equippedEnv);
      this.ui.showMenu(await this.checkResumable());
    } catch (e) {
      this.ui.toast(errText(e));
    } finally {
      this.busy = false;
    }
  }

  /**
   * Call something out to the table. The server has the real limit; refusing
   * here as well only keeps the button from promising something it cannot do.
   */
  private async sendTaunt(id: string): Promise<void> {
    if (!this.gameId || this.watching || this.taunts.cooldownLeft() > 0) return;
    try {
      await api.taunt(this.gameId, this.token, id);
      this.taunts.noteSent();
    } catch (e) {
      this.taunts.noteRefused();
      this.ui.toast(errText(e));
    }
    this.refreshTauntButton();
  }

  /** Keeps the button in step with the cooldown, once a second is plenty. */
  private refreshTauntButton(): void {
    const st = this.state;
    const usable =
      !!st && st.status === "active" && st.mode === "online" && !this.watching && st.you !== "all";
    this.ui.setTauntState(usable, this.taunts.cooldownLeft());
  }

  private async setCountry(code: string): Promise<void> {
    const p = this.profile;
    if (!p || this.busy) return;
    this.busy = true;
    try {
      this.profile = await api.updateProfile(p.token, p.name, code);
      this.ui.setData(this.profile, this.catalog);
      this.ui.toast(code ? "Your flag is set." : "Flag cleared.", true);
    } catch (e) {
      this.ui.toast(errText(e));
    } finally {
      this.busy = false;
    }
  }

  private signOut(): void {
    localStorage.removeItem(STORE.profile);
    this.forgetGame();
    this.profile = null;
    this.ui.setData(null, this.catalog);
    this.ui.showSignIn();
  }

  private async checkResumable(): Promise<boolean> {
    const id = localStorage.getItem(STORE.game);
    const token = localStorage.getItem(STORE.token);
    if (!id || !token) return false;
    try {
      const st = await api.getGame(id, token);
      if (st.status !== "finished") return true;
    } catch {
      /* gone */
    }
    this.forgetGame();
    return false;
  }

  // ---- environment & collection ----

  applyEnvironment(envId: string): void {
    const id = envId || "picnic";
    if (id === this.envId) return;
    this.envId = id;
    this.env?.dispose();
    // Environments with a downloaded model arrive a beat later; say so rather
    // than leaving the player staring at an empty field.
    const heavy = id === "dust2";
    if (heavy) this.ui.toast("Planting the board…");
    this.env = buildEnvironment(this.scene, id, () => {
      if (this.envId === id && heavy) this.ui.toast("Bombsite B secured.", true);
    });
    this.shadows = this.env.shadows;
    for (const m of this.casters) this.shadows?.addShadowCaster(m);
  }

  /**
   * Swaps the board finish. The colour is baked into the cloth material when
   * the board is built, so changing it means rebuilding — cheap, and it keeps
   * the build path single rather than having a second "recolour" path that
   * can drift from it.
   */
  private applyBoardFinish(id: string): void {
    if (!this.board.setBoardFinish(id) || !this.boardDto) return;
    this.board.build(this.boardDto);
    if (this.state) void this.board.sync(this.state, false);
  }

  private registerCasters(meshes: Mesh[]): void {
    for (const m of meshes) {
      this.casters.push(m);
      this.shadows?.addShadowCaster(m);
    }
  }

  private async equip(id: string, kind: CatalogItem["kind"]): Promise<void> {
    if (!this.profile || this.busy) return;
    try {
      const what = kind === "skin" ? { skin: id } : kind === "board" ? { board: id } : { env: id };
      this.profile = await api.equip(this.profile.token, what);
      if (kind === "env") this.applyEnvironment(this.profile.equippedEnv);
      if (kind === "board") this.applyBoardFinish(this.profile.equippedBoard);
      this.ui.setData(this.profile, this.catalog);
      this.ui.toast(
        kind === "env"
          ? "The scenery shifts around you."
          : kind === "board"
            ? "A different cloth is laid out."
            : "Your host dons a new look for the next battle.",
        true,
      );
    } catch (e) {
      this.ui.toast(errText(e));
    }
  }

  private async refreshProfile(): Promise<void> {
    if (!this.profile) return;
    const before = new Set(this.profile.unlocked);
    try {
      this.profile = await api.getProfile(this.profile.token);
    } catch {
      return;
    }
    this.ui.setData(this.profile, this.catalog);
    for (const id of this.profile.unlocked) {
      if (before.has(id)) continue;
      const item = this.catalog.find((i) => i.id === id);
      if (item) this.ui.toast(`Unlocked: ${item.name}! Find it in the Collection.`, true);
    }
  }

  // ---- game lifecycle ----

  private get profileToken(): string {
    return this.profile?.token ?? "";
  }

  // ---- the board room ----

  private async showLeaderboard(): Promise<void> {
    try {
      this.ui.showStandings(await api.leaderboard(this.profileToken));
    } catch (e) {
      this.ui.toast(errText(e));
    }
  }

  private async openBrowser(): Promise<void> {
    this.ui.showBrowser();
    await this.refreshLobbies();
    this.startLobbyPolling();
  }

  private async refreshLobbies(): Promise<void> {
    try {
      const { lobbies, running } = await api.lobbies(this.profileToken);
      if (this.ui.browsing()) this.ui.renderLobbies(lobbies, running);
    } catch (e) {
      console.warn("lobby list failed:", e);
    }
  }

  private startLobbyPolling(): void {
    this.stopLobbyPolling();
    this.lobbyTimer = window.setInterval(() => {
      if (!this.ui.browsing()) {
        this.stopLobbyPolling();
        return;
      }
      void this.refreshLobbies();
    }, LOBBY_MS);
  }

  private stopLobbyPolling(): void {
    if (this.lobbyTimer) clearInterval(this.lobbyTimer);
    this.lobbyTimer = 0;
  }

  private async createGame(opts: {
    mode: "online" | "offline";
    players: number;
    name?: string;
    password?: string;
  }): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    this.stopLobbyPolling();
    try {
      this.enterGame(await api.createGame({ ...opts, profile: this.profileToken }));
    } catch (e) {
      this.ui.toast(errText(e));
    } finally {
      this.busy = false;
    }
  }

  private async createTable(opts: { name: string; players: number; password: string }): Promise<void> {
    await this.createGame({ mode: "online", ...opts });
  }

  private async joinGame(gameId: string, password: string): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    this.stopLobbyPolling();
    try {
      // A table you already sit at is one to walk back to, not to sit down at
      // twice: use the seat token the server gave us rather than asking for
      // another seat (which it now refuses anyway).
      const savedId = localStorage.getItem(STORE.game);
      const savedToken = localStorage.getItem(STORE.token);
      if (savedId === gameId && savedToken) {
        this.token = savedToken;
        this.enterGame(await api.getGame(gameId, savedToken));
        return;
      }
      this.enterGame(await api.joinGame(gameId, password, this.profileToken));
    } catch (e) {
      const msg = errText(e);
      // A wrong password should leave the prompt up rather than dumping the
      // player back to the list to start over.
      if (e instanceof ApiError && e.status === 403) this.ui.passwordRejected(msg);
      else this.ui.toast(msg);
      if (this.ui.browsing()) this.startLobbyPolling();
    } finally {
      this.busy = false;
    }
  }

  /**
   * Ask for another game against the same people. Whoever asks first makes the
   * table; anyone asking afterwards is seated at it, so pressing this is both
   * "offer" and "accept" depending on who got there first.
   */
  private async rematch(): Promise<void> {
    if (!this.gameId || this.busy) return;
    this.busy = true;
    try {
      const st = await api.rematch(this.gameId, this.token);
      this.rematchOffered = false;
      const waiting = st.status === "waiting";
      this.enterGame(st);
      if (waiting) this.ui.rematchWaiting();
    } catch (e) {
      this.ui.toast(errText(e));
    } finally {
      this.busy = false;
    }
  }

  /**
   * Watch a game somebody else is playing. There is no seat and no token, so
   * every move endpoint keeps refusing us for the same reason it always did —
   * a spectator is just somebody the game does not recognise.
   */
  private async watchGame(id: string): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    this.stopLobbyPolling();
    try {
      const st = await api.watchGame(id);
      if (!st) throw new Error("that game is not being shown");
      this.watching = true;
      this.gameId = id;
      this.token = "";
      this.state = null;
      this.deselect();
      this.board.clear();
      this.ui.showWatching();
      await this.applyState(st, false);
      this.focusCamera(true);
      this.startWatchPolling();
    } catch (e) {
      this.ui.toast(errText(e));
      if (this.ui.browsing()) this.startLobbyPolling();
    } finally {
      this.busy = false;
    }
  }

  private startWatchPolling(): void {
    this.stopPolling();
    this.pollTimer = window.setInterval(() => {
      void (async () => {
        if (!this.gameId || this.animating) return;
        try {
          const st = await api.watchGame(this.gameId, this.state?.version ?? 0);
          if (st) await this.applyState(st, true);
        } catch (e) {
          console.warn("watch poll failed:", e);
        }
      })();
    }, POLL_MS);
  }

  /** Begin a table that never filled up; the computer takes what is left. */
  private async startEarly(): Promise<void> {
    if (!this.gameId || this.busy) return;
    this.busy = true;
    try {
      await this.applyState(await api.startEarly(this.gameId, this.token), false);
      this.ui.toast("The empty seats are taken by the house.", true);
    } catch (e) {
      this.ui.toast(errText(e));
    } finally {
      this.busy = false;
    }
  }

  private async resume(): Promise<void> {
    const id = localStorage.getItem(STORE.game);
    const token = localStorage.getItem(STORE.token);
    if (!id || !token) return;
    this.busy = true;
    try {
      const st = await api.getGame(id, token);
      this.token = token;
      this.enterGame(st);
    } catch {
      this.forgetGame();
      this.ui.toast("That campaign is lost to history.");
      this.ui.showMenu(false);
    } finally {
      this.busy = false;
    }
  }

  private enterGame(state: GameState): void {
    if (state.token) this.token = state.token;
    this.gameId = state.gameId;
    this.rematchOffered = false;
    this.watching = false;
    this.taunts.reset();
    this.stopLobbyPolling();
    localStorage.setItem(STORE.game, this.gameId);
    localStorage.setItem(STORE.token, this.token);

    this.state = null;
    this.deselect();
    this.board.clear();
    this.ui.showGame();
    void this.applyState(state, false);
    this.focusCamera(true);
    this.startPolling();
  }

  private leaveToMenu(): void {
    this.stopPolling();
    this.stopLobbyPolling();
    clearInterval(this.clockTimer);
    this.clockTimer = 0;
    this.rematchOffered = false;
    this.watching = false;
    this.state = null;
    this.gameId = "";
    this.token = "";
    this.deselect();
    this.board.clear();
    this.forgetGame();
    void this.refreshProfile();
    this.ui.showMenu(false);
  }

  private forgetGame(): void {
    localStorage.removeItem(STORE.game);
    localStorage.removeItem(STORE.token);
  }

  private async resign(): Promise<void> {
    if (!this.state || this.state.status !== "active") return;
    try {
      await this.applyState(await api.resign(this.gameId, this.token), false);
    } catch (e) {
      this.ui.toast(errText(e));
    }
  }

  private startPolling(): void {
    this.stopPolling();
    this.pollTimer = window.setInterval(() => void this.poll(), POLL_MS);
  }

  private stopPolling(): void {
    if (this.pollTimer) clearInterval(this.pollTimer);
    this.pollTimer = 0;
  }

  private async poll(): Promise<void> {
    if (!this.gameId || this.busy || this.animating || this.dragging) return;
    try {
      const st = await api.pollGame(this.gameId, this.token, this.state?.version ?? 0);
      if (st) await this.applyState(st, true);
    } catch (e) {
      // Usually a blip between ticks — but never swallow it silently, or a
      // real fault looks exactly like a quiet game.
      console.warn("poll failed:", e);
    }
  }

  /**
   * Applies a new state. Calls are queued: a move committed while an opponent
   * animation is still walking must not interleave with it, and the animation
   * flag has to be cleared even when something throws (a stuck flag would
   * freeze both the HUD and polling).
   */
  private applyState(state: GameState, animate: boolean): Promise<void> {
    this.applyQueue = this.applyQueue.then(
      () => this.applyStateNow(state, animate),
      () => this.applyStateNow(state, animate),
    );
    return this.applyQueue;
  }

  private async applyStateNow(state: GameState, animate: boolean): Promise<void> {
    if (this.state && state.version <= this.state.version) return;
    const hadPrevious = this.state !== null;
    this.state = state;
    this.deselect();
    this.board.setSkins(state);

    const walk = animate && hadPrevious && hasPath(state.lastMove);
    try {
      if (walk) {
        this.animating = true;
        this.board.clearTrail();
      }
      await this.board.sync(state, walk);
    } finally {
      this.animating = false;
    }

    if (hasPath(state.lastMove)) this.board.showTrail(state.lastMove.path);
    this.playIncomingTaunt(state);
    if (this.watching) {
      this.ui.updateWhileWatching(state);
      if (state.status === "finished") this.stopPolling();
      return;
    }
    this.ui.updateFromState(state, this.myTurn());
    this.refreshTauntButton();
    await this.syncDicePhase(state);

    this.syncClock(state);

    if (state.status === "finished") {
      this.stopPolling();
      const title = this.resultTitle(state);
      this.ui.status(title);
      this.ui.showResult(title, REASONS[state.winReason] ?? state.winReason);
      // Keep polling the finished game, quietly: that is how we hear about a
      // rematch somebody else asked for. `forgetGame` would throw away the
      // token we need to accept it.
      this.watchForRematch();
      void this.refreshProfile();
    }
  }

  /**
   * Plays whatever was called out at the table, once. Your own line is shown
   * too — you said it out loud, so seeing it is the confirmation that it left.
   * The sound follows the music mute, since both are "noise from the game".
   */
  private playIncomingTaunt(state: GameState): void {
    const fresh = this.taunts.receive(state.taunt, this.music.isMuted);
    if (!fresh) return;
    const from = state.seats.find((s) => s.seat === fresh.seat);
    this.ui.showTaunt(fresh.seat, from?.name ?? "", fresh.text);
  }

  /** Ticks the visible countdown between polls, so it moves once a second. */
  private syncClock(state: GameState): void {
    clearInterval(this.clockTimer);
    this.clockTimer = 0;
    const mine = state.status === "active" && this.myTurn();
    const deadline = state.deadline ?? "";
    this.ui.setClock(deadline, mine);
    if (!deadline || !mine) return;
    this.clockTimer = window.setInterval(() => this.ui.setClock(deadline, true), 1000);
  }

  /**
   * After a game ends, watch it for a rematch. Cheap — one poll every few
   * seconds while somebody sits on the result screen — and it is what lets a
   * rematch be an agreement rather than a link you have to send.
   */
  private watchForRematch(): void {
    this.stopPolling();
    this.pollTimer = window.setInterval(() => {
      void (async () => {
        if (!this.gameId || this.busy) return;
        try {
          const st = await api.getGame(this.gameId, this.token);
          if (st.rematchId && !this.rematchOffered) {
            this.rematchOffered = true;
            this.ui.rematchOffered();
          }
        } catch {
          this.stopPolling(); // the table has been swept; nothing to wait for
        }
      })();
    }, 3000);
  }

  /**
   * Keeps the die on screen while the table is rolling. It hovers, waiting to
   * be thrown; `throwDie` does the throwing.
   */
  private async syncDicePhase(state: GameState): Promise<void> {
    if (state.status !== "active" || state.phase !== "roll") {
      if (this.diceRoller.waiting) this.diceRoller.clear();
      return;
    }
    if (!state.yourRoll) {
      // Someone else's throw: clear our die and wait for the poll.
      if (this.diceRoller.waiting) this.diceRoller.clear();
      return;
    }
    if (this.diceRoller.waiting) return;
    const seat = this.rollingSeat(state);
    if (seat) await this.diceRoller.present(seat);
  }

  /** Which seat we are about to throw for. */
  private rollingSeat(state: GameState): Seat | null {
    const pending = state.pending ?? [];
    if (!pending.length) return null;
    if (state.you === "all") return pending[0]!;
    return pending.find((s) => s === state.you) ?? null;
  }

  /** The player's throw: ask the server for a value, then land the die on it. */
  async rollDie(): Promise<void> {
    const st = this.state;
    if (!st || st.phase !== "roll" || !st.yourRoll || this.busy) return;
    if (!this.diceRoller.waiting) return;
    const seat = this.rollingSeat(st);
    if (!seat) return;

    this.busy = true;
    try {
      const res = await api.roll(this.gameId, this.token, seat);
      await this.diceRoller.rollTo(res.rolled);
      this.ui.toast(`${seatName(res.rolledSeat)} threw a ${res.rolled}`, true);
      await new Promise((r) => setTimeout(r, 600));
      this.diceRoller.clear();
      await this.applyState(res, false);
      if (res.phase === "play") {
        this.ui.toast(`${seatName(res.turn)} won the roll and opens`, true);
        this.focusCamera();
      }
    } catch (e) {
      this.ui.toast(errText(e));
    } finally {
      this.busy = false;
    }
  }

  private resultTitle(state: GameState): string {
    if (!state.winner) return "A weary peace";
    if (state.you === "all" || state.mode === "offline") return `${seatName(state.winner)} is victorious`;
    return state.winner === state.you ? "Victory is yours" : `${seatName(state.winner)} is victorious`;
  }

  // ---- camera ----

  /** The seat the camera should sit behind: yours, or whoever is to move. */
  private mySide(): Seat {
    const st = this.state;
    if (!st) return "west";
    if (st.you === "all" || st.you === "") return st.turn;
    return st.you;
  }

  private focusCamera(instant = false): void {
    const targetAlpha = seatInfo(this.mySide()).alpha;
    // Take the shortest way round rather than unwinding a full turn.
    const current = this.camera.alpha;
    const wrapped = targetAlpha + Math.round((current - targetAlpha) / (Math.PI * 2)) * Math.PI * 2;
    if (instant) {
      this.camera.alpha = wrapped;
      this.camera.beta = 0.95;
      this.camera.radius = 15;
      return;
    }
    this.tweenCamera(wrapped, 0.95, 15);
  }

  private tweenCamera(alpha: number, beta: number, radius: number): void {
    const from = { a: this.camera.alpha, b: this.camera.beta, r: this.camera.radius };
    let elapsed = 0;
    const duration = 620;
    const observer = this.scene.onBeforeRenderObservable.add(() => {
      elapsed += this.engine.getDeltaTime();
      const t = Math.min(1, elapsed / duration);
      const e = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
      this.camera.alpha = from.a + (alpha - from.a) * e;
      this.camera.beta = from.b + (beta - from.b) * e;
      this.camera.radius = from.r + (radius - from.r) * e;
      if (t >= 1) this.scene.onBeforeRenderObservable.remove(observer);
    });
  }

  // ---- input ----

  private myTurn(): boolean {
    const st = this.state;
    return !!st && st.status === "active" && (st.you === "all" || st.you === st.turn);
  }

  /** Only the seat on the move may be picked up — knocked-out seats never. */
  private isMine(owner: Seat): boolean {
    const st = this.state;
    if (!st) return false;
    if (st.seats.some((s) => s.seat === owner && s.out)) return false;
    if (st.you === "all") return owner === st.turn;
    return owner === st.you;
  }

  private planePoint(): Vector3 | null {
    const ray = this.scene.createPickingRay(this.scene.pointerX, this.scene.pointerY, Matrix.Identity(), this.camera);
    const d = ray.intersectsPlane(GROUND);
    return d === null ? null : ray.origin.add(ray.direction.scale(d));
  }

  private onPointer(pi: PointerInfo): void {
    switch (pi.type) {
      case PointerEventTypes.POINTERDOWN:
        this.onDown(pi);
        break;
      case PointerEventTypes.POINTERMOVE:
        this.onMove();
        break;
      case PointerEventTypes.POINTERUP:
        void this.onUp(pi);
        break;
    }
  }

  private onDown(pi: PointerInfo): void {
    const ev = pi.event as PointerEvent;
    if (ev.button !== 0) return;
    // A press arriving while a stone is already in hand is not a new gesture
    // (browsers and pen/touch stacks do emit these); dropping the stone here
    // would make the drag fall apart mid-flight.
    if (this.dragging) return;
    this.pressPiece = null;
    this.pressX = this.scene.pointerX;
    this.pressY = this.scene.pointerY;
    if (!this.state || this.state.status !== "active" || this.animating || this.busy) return;

    // While the table is rolling, a press on the die throws it.
    if (this.state.phase === "roll") {
      const die = this.scene.pick(
        this.scene.pointerX,
        this.scene.pointerY,
        (m) => !!(m.metadata as { die?: boolean } | undefined)?.die,
      );
      if (die?.hit) void this.rollDie();
      return;
    }

    const hit = this.scene.pick(
      this.scene.pointerX,
      this.scene.pointerY,
      (m) => !!m.metadata && (!!m.metadata.pieceId || !!m.metadata.hintNode),
    );
    const meta = hit?.pickedMesh?.metadata as { pieceId?: string; hintNode?: string } | undefined;

    // Tapping a lit field completes a click-click move.
    if (meta?.hintNode && this.selected) {
      void this.commitMove(this.selected, meta.hintNode, false);
      return;
    }

    // Tapping the stone you can take is the same thing. The enemy piece sits
    // on top of its own strike ring, so the pick lands on the piece and never
    // reaches the marker underneath — without this, a capture could only be
    // made by dragging.
    if (meta?.pieceId && this.selected) {
      const target = this.board.pieceById(meta.pieceId);
      if (target && this.options.some((o) => o.to === target.node)) {
        void this.commitMove(this.selected, target.node, false);
        return;
      }
    }

    if (meta?.pieceId && this.myTurn()) {
      const pv = this.board.pieceById(meta.pieceId);
      if (pv && this.isMine(pv.owner)) {
        this.pressPiece = meta.pieceId;
        this.setCameraControl(false); // orbit must not fight the drag
        if (this.selected !== pv.node) void this.selectPiece(pv.node);
        return;
      }
    }
    // Anything else: the camera keeps its grip and orbits.
  }

  private onMove(): void {
    if (!this.pressPiece) return;
    const pv = this.board.pieceById(this.pressPiece);
    if (!pv) return;

    if (!this.dragging) {
      const dx = this.scene.pointerX - this.pressX;
      const dy = this.scene.pointerY - this.pressY;
      if (Math.hypot(dx, dy) < DRAG_THRESHOLD) return;
      this.dragging = true;
      pv.lift();
    }
    const point = this.planePoint();
    if (!point) return;
    pv.follow(point);
    this.board.emphasize(this.nearestOption(point, 1.0));
  }

  private async onUp(pi: PointerInfo): Promise<void> {
    // Only a genuine release reports no buttons still held; anything else
    // mid-drag is a stray event and must not drop the stone.
    if (this.dragging && (pi.event as PointerEvent).buttons !== 0) return;

    const pieceId = this.pressPiece;
    this.pressPiece = null;
    if (!this.dragging) {
      this.setCameraControl(true);
      return;
    }
    this.dragging = false;
    this.setCameraControl(true);
    this.board.emphasize(null);

    const pv = pieceId ? this.board.pieceById(pieceId) : undefined;
    if (!pv) return;
    const point = this.planePoint();
    const target = point ? this.nearestOption(point, 1.0) : null;
    const from = this.selected;

    if (target && from) {
      await pv.dropTo(this.board.nodeWorld(target));
      await this.commitMove(from, target, true);
    } else {
      await pv.dropTo(this.board.nodeWorld(pv.node)); // return it to its field
    }
  }

  // Attaching twice registers a second set of input handlers (which makes the
  // camera spin at double speed), so track the state instead of toggling blind.
  private setCameraControl(enabled: boolean): void {
    if (enabled && this.cameraDetached) {
      this.camera.attachControl(true);
      this.cameraDetached = false;
    } else if (!enabled && !this.cameraDetached) {
      this.camera.detachControl();
      this.cameraDetached = true;
    }
  }

  private nearestOption(point: Vector3, max: number): string | null {
    let best: string | null = null;
    let bestD = max * max;
    for (const o of this.options) {
      const w = this.board.nodeWorld(o.to);
      const d = (w.x - point.x) ** 2 + (w.z - point.z) ** 2;
      if (d < bestD) {
        bestD = d;
        best = o.to;
      }
    }
    return best;
  }

  private async selectPiece(node: string): Promise<void> {
    this.selected = node;
    this.options = [];
    this.board.select(node);
    this.board.clearHints();
    try {
      const moves = await api.legalMoves(this.gameId, this.token, node);
      if (this.selected !== node) return; // selection changed while waiting
      this.options = moves;
      if (!moves.length) this.ui.toast("That stone is walled in — no legal moves.");
      this.board.showHints(moves);
    } catch (e) {
      this.ui.toast(errText(e));
    }
  }

  private deselect(): void {
    this.selected = null;
    this.options = [];
    this.board.clearHints();
    this.board.select(null);
  }

  private async commitMove(from: string, to: string, alreadyPlaced: boolean): Promise<void> {
    this.busy = true;
    this.deselect();
    try {
      const st = await api.move(this.gameId, this.token, from, to);
      await this.applyState(st, !alreadyPlaced);
    } catch (e) {
      this.ui.toast(errText(e));
      if (this.state) await this.board.sync(this.state, false); // snap back
    } finally {
      this.busy = false;
    }
  }

  /** Screen position of a board field — debug hook, also handy for tests. */
  screenOf(node: string): { x: number; y: number } | null {
    if (!this.board.hasNode(node)) return null;
    const w = this.board.nodeWorld(node);
    // Compose view*projection here rather than trusting the scene's cached
    // transform, which is only valid mid-render.
    const transform = this.camera.getViewMatrix().multiply(this.camera.getProjectionMatrix());
    const p = Vector3.Project(
      new Vector3(w.x, 0.1, w.z),
      Matrix.Identity(),
      transform,
      this.camera.viewport.toGlobal(this.engine.getRenderWidth(), this.engine.getRenderHeight()),
    );
    const rect = this.engine.getRenderingCanvas()!.getBoundingClientRect();
    return { x: rect.left + p.x, y: rect.top + p.y };
  }
}

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  return e instanceof Error ? e.message : "something went wrong";
}

/**
 * Steam hands the browser back with #steam=<token>. Read it once and wipe it,
 * so a refresh (or a shared URL) never carries someone's session token.
 */
function readSteamFragment(): string {
  const match = /(?:^|[#&])steam=([^&]+)/.exec(window.location.hash);
  if (!match) return "";
  history.replaceState(null, "", window.location.pathname + window.location.search);
  return decodeURIComponent(match[1]!);
}

const canvas = document.getElementById("scene") as HTMLCanvasElement;
const game = new Kingsreach(canvas);
// Debug handle: lets you poke at the scene from the console (and drives the
// end-to-end browser tests). Harmless — the server validates every move.
(window as unknown as { kingsreach: Kingsreach }).kingsreach = game;
void game.start();
