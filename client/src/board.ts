import { MeshBuilder, TransformNode, Vector3 } from "./babylon";
import type { GlowLayer, Mesh, Scene } from "./babylon";
import { C, disc, hexPrism, mat, ring } from "./theme";
import { PIECE_H, buildPiece } from "./skins";
import { seatInfo } from "./seats";
import type { BoardDto, GameState, MoveOption, PieceDto, Seat } from "./api";
import { hasPath } from "./api";

const LIFT = 0.55; // how high a held stone floats above the cloth

function ease(t: number): number {
  return t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
}

// Frame-based tween driven by the render loop, with a wall-clock safety net:
// a hidden browser tab stops producing frames, and without this the promise
// would never settle — stalling the move queue until the tab is focused again.
function tween(scene: Scene, ms: number, step: (t: number) => void): Promise<void> {
  return new Promise((resolve) => {
    let elapsed = 0;
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      scene.onBeforeRenderObservable.remove(observer);
      clearTimeout(timer);
      step(1);
      resolve();
    };
    const observer = scene.onBeforeRenderObservable.add(() => {
      elapsed += scene.getEngine().getDeltaTime();
      const t = Math.min(1, elapsed / ms);
      step(t);
      if (t >= 1) finish();
    });
    const timer = setTimeout(finish, ms + 400);
  });
}

class PieceView {
  readonly root: TransformNode;
  meshes: Mesh[] = [];
  held = false;

  constructor(
    private readonly scene: Scene,
    readonly id: string,
    readonly owner: Seat,
    readonly value: number,
    public node: string,
    skin: string,
    position: Vector3,
  ) {
    this.root = new TransformNode("piece-" + id, scene);
    this.root.position = position.clone();
    this.meshes = buildPiece(scene, this.root as unknown as Mesh, skin, owner, value);
    for (const m of this.meshes) {
      m.metadata = { pieceId: id };
      m.receiveShadows = true;
    }
  }

  setPosition(p: Vector3): void {
    this.root.position.copyFrom(p);
  }

  lift(): void {
    this.held = true;
    this.root.position.y = LIFT;
  }

  follow(point: Vector3): void {
    this.root.position.x += (point.x - this.root.position.x) * 0.45;
    this.root.position.z += (point.z - this.root.position.z) * 0.45;
    this.root.position.y = LIFT;
  }

  async dropTo(target: Vector3): Promise<void> {
    this.held = false;
    const from = this.root.position.clone();
    await tween(this.scene, 130, (t) => {
      const e = ease(t);
      this.root.position.set(
        from.x + (target.x - from.x) * e,
        from.y + (target.y - from.y) * e,
        from.z + (target.z - from.z) * e,
      );
    });
  }

  // Hops node to node along the server-provided path.
  async hopAlong(points: Vector3[]): Promise<void> {
    for (let i = 1; i < points.length; i++) {
      const a = this.root.position.clone();
      const b = points[i]!;
      await tween(this.scene, 170, (t) => {
        const e = ease(t);
        this.root.position.set(
          a.x + (b.x - a.x) * e,
          a.y + (b.y - a.y) * e + Math.sin(t * Math.PI) * 0.32,
          a.z + (b.z - a.z) * e,
        );
      });
      this.root.position.copyFrom(b);
    }
  }

  async vanish(): Promise<void> {
    const start = this.root.scaling.clone();
    await tween(this.scene, 180, (t) => {
      const s = 1 - t;
      this.root.scaling.set(start.x * s, start.y * s, start.z * s);
      this.root.position.y = t * 0.35;
    });
  }

  dispose(): void {
    this.meshes.forEach((m) => m.dispose());
    this.root.dispose();
  }
}

export class BoardView {
  private readonly nodePos = new Map<string, Vector3>();
  private readonly pieces = new Map<string, PieceView>();
  private readonly hints = new Map<string, Mesh>();
  private readonly captureNodes = new Set<string>();
  /** Set by the app so strike markers can opt out of the bloom. */
  glow: GlowLayer | null = null;
  private trail: Mesh[] = [];
  private selectionRing: Mesh | null = null;
  private staticRoot: TransformNode | null = null;
  private pieceRoot!: TransformNode;
  private graveRoot!: TransformNode;
  private readonly graves = new Map<string, TransformNode>();
  private skins = new Map<Seat, string>();
  private pulse = 0;

  /** Called whenever meshes appear so the caller can register shadow casters. */
  onMeshes: (meshes: Mesh[]) => void = () => {};

  constructor(private readonly scene: Scene) {
    scene.onBeforeRenderObservable.add(() => this.animateHints());
  }

  build(board: BoardDto): void {
    this.staticRoot?.dispose();
    const root = new TransformNode("board", this.scene);
    this.staticRoot = root;
    this.nodePos.clear();
    for (const n of board.nodes) this.nodePos.set(n.id, new Vector3(n.x, 0, n.y));

    const created: Mesh[] = [];

    const seam = hexPrism(this.scene, "cloth-seam", 5.62, 0.2, mat(this.scene, "seam", C.clothEdge, { roughness: 0.9 }));
    seam.parent = root;
    seam.position.y = -0.115;
    seam.receiveShadows = true;

    const cloth = hexPrism(this.scene, "cloth", 5.4, 0.16, mat(this.scene, "cloth", C.cloth, { roughness: 0.92 }));
    cloth.parent = root;
    cloth.position.y = -0.08;
    cloth.receiveShadows = true;
    created.push(seam, cloth);

    const tealMat = mat(this.scene, "stitch", C.teal, { emissive: C.teal.scale(0.16), roughness: 0.5 });
    const goldMat = mat(this.scene, "gold-stitch", C.gold, { emissive: C.gold.scale(0.5), roughness: 0.25 });
    for (const e of board.edges) {
      const a = this.nodePos.get(e.a)!;
      const b = this.nodePos.get(e.b)!;
      const len = Vector3.Distance(a, b);
      const bar = MeshBuilder.CreateBox(
        e.gold ? "gold-edge" : "edge",
        { width: len, height: e.gold ? 0.035 : 0.022, depth: e.gold ? 0.09 : 0.055 },
        this.scene,
      );
      bar.material = e.gold ? goldMat : tealMat;
      bar.parent = root;
      bar.position.set((a.x + b.x) / 2, e.gold ? 0.018 : 0.011, (a.z + b.z) / 2);
      bar.rotation.y = Math.atan2(-(b.z - a.z), b.x - a.x);
      bar.isPickable = false;
    }

    const nodeMat = mat(this.scene, "node", C.node, { roughness: 0.6 });
    const throneMat = mat(this.scene, "throne", C.gold, { emissive: C.gold.scale(0.55), roughness: 0.2 });
    const throneCore = mat(this.scene, "throne-core", C.goldDeep, { emissive: C.goldDeep.scale(0.4), roughness: 0.3 });
    for (const n of board.nodes) {
      const p = this.nodePos.get(n.id)!;
      if (n.center) {
        const t = disc(this.scene, "throne", 0.3, 0.03, throneMat);
        t.parent = root;
        t.position.set(p.x, 0.02, p.z);
        t.isPickable = false;
        const core = disc(this.scene, "throne-core", 0.1, 0.05, throneCore);
        core.parent = root;
        core.position.set(p.x, 0.035, p.z);
        core.isPickable = false;
      } else {
        const d = disc(this.scene, "node", 0.1, 0.026, nodeMat);
        d.parent = root;
        d.position.set(p.x, 0.014, p.z);
        d.isPickable = false;
      }
    }

    this.pieceRoot = new TransformNode("pieces", this.scene);
    this.graveRoot = new TransformNode("graveyard", this.scene);
    this.selectionRing = MeshBuilder.CreateTorus(
      "selection",
      { diameter: 0.78, thickness: 0.05, tessellation: 32 },
      this.scene,
    );
    this.selectionRing.material = mat(this.scene, "sel", C.hint, { emissive: C.hint.scale(0.9) });
    this.selectionRing.isPickable = false;
    this.selectionRing.setEnabled(false);

    this.onMeshes(created);
  }

  hasNode(id: string): boolean {
    return this.nodePos.has(id);
  }

  nodeWorld(id: string): Vector3 {
    return this.nodePos.get(id) ?? Vector3.Zero();
  }

  /** Nearest board field to a point on the cloth plane, within `max` units. */
  nodeAt(point: Vector3, max = 0.5): string | null {
    let best: string | null = null;
    let bestD = max * max;
    for (const [id, p] of this.nodePos) {
      const d = (p.x - point.x) ** 2 + (p.z - point.z) ** 2;
      if (d < bestD) {
        bestD = d;
        best = id;
      }
    }
    return best;
  }

  pieceById(id: string): PieceView | undefined {
    return this.pieces.get(id);
  }

  pieceAt(node: string): PieceView | undefined {
    for (const p of this.pieces.values()) if (p.node === node) return p;
    return undefined;
  }

  /** Applies each seat's chosen skin, rebuilding pieces when one changes. */
  setSkins(state: GameState): void {
    let changed = false;
    for (const seat of state.seats) {
      const skin = seat.skin || "clay";
      if (this.skins.get(seat.seat) !== skin) {
        this.skins.set(seat.seat, skin);
        changed = true;
      }
    }
    if (!changed) return;
    for (const p of this.pieces.values()) p.dispose(); // rebuilt on next sync
    this.pieces.clear();
    this.clearGraves();
  }

  private skinFor(owner: Seat): string {
    return this.skins.get(owner) ?? "clay";
  }

  /** Reconciles piece views with the state, optionally walking the last move. */
  async sync(state: GameState, animate: boolean): Promise<void> {
    if (animate && hasPath(state.lastMove) && this.pieces.has(state.lastMove.piece)) {
      const mv = state.lastMove;
      const mover = this.pieces.get(mv.piece)!;
      const points = mv.path.filter((id) => this.nodePos.has(id)).map((id) => this.nodeWorld(id));
      await mover.hopAlong(points);
      const victim = mv.captured ? this.pieces.get(mv.captured) : undefined;
      if (victim) {
        await victim.vanish();
        victim.dispose();
        this.pieces.delete(mv.captured);
      }
    }
    this.applyState(state);
  }

  private applyState(state: GameState): void {
    const seen = new Set<string>();
    const fresh: Mesh[] = [];
    for (const dto of state.pieces) {
      if (dto.captured) {
        this.removePiece(dto.id);
        continue;
      }
      seen.add(dto.id);
      let pv = this.pieces.get(dto.id);
      if (!pv) {
        pv = this.createPiece(dto);
        fresh.push(...pv.meshes);
      }
      pv.node = dto.node;
      if (!pv.held) pv.setPosition(this.nodeWorld(dto.node));
    }
    for (const id of [...this.pieces.keys()]) if (!seen.has(id)) this.removePiece(id);
    if (fresh.length) this.onMeshes(fresh);
    this.syncGraveyard(state);
  }

  // ---- graveyard: struck stones laid out beside their owner's edge ----

  private syncGraveyard(state: GameState): void {
    const perOwner = new Map<Seat, PieceDto[]>();
    for (const p of state.pieces) {
      if (!p.captured) continue;
      const list = perOwner.get(p.owner) ?? [];
      list.push(p);
      perOwner.set(p.owner, list);
    }

    const wanted = new Set<string>();
    const fresh: Mesh[] = [];
    for (const [owner, lost] of perOwner) {
      const info = seatInfo(owner);
      // Rows run along the board edge, stepping outward as they fill up.
      const outward = new Vector3(info.dir.x, 0, info.dir.z);
      const along = new Vector3(-info.dir.z, 0, info.dir.x);
      lost.sort((a, b) => a.id.localeCompare(b.id));
      lost.forEach((p, i) => {
        wanted.add(p.id);
        if (this.graves.has(p.id)) return;
        const col = i % 4;
        const row = Math.floor(i / 4);
        const pos = outward
          .scale(6.55 + row * 0.85)
          .add(along.scale((col - 1.5) * 0.82))
          .add(new Vector3(0, -0.02, 0));
        const node = this.makeGravePiece(p, pos, fresh);
        this.graves.set(p.id, node);
      });
    }
    for (const [id, node] of this.graves) {
      if (wanted.has(id)) continue;
      node.getChildMeshes().forEach((m) => m.dispose());
      node.dispose();
      this.graves.delete(id);
    }
    if (fresh.length) this.onMeshes(fresh);
  }

  private makeGravePiece(dto: PieceDto, pos: Vector3, fresh: Mesh[]): TransformNode {
    const root = new TransformNode("grave-" + dto.id, this.scene);
    root.parent = this.graveRoot;
    root.position = pos;
    // A small slab so the stones read as "set aside" rather than dropped.
    const slab = disc(
      this.scene,
      "grave-slab",
      0.38,
      0.04,
      mat(this.scene, "grave-slab", seatInfo(dto.owner).hue.scale(0.35), { roughness: 0.9, alpha: 0.85 }),
    );
    slab.parent = root;
    slab.position.y = 0.02;
    slab.isPickable = false;
    const parts = buildPiece(this.scene, root as unknown as Mesh, this.skinFor(dto.owner), dto.owner, dto.value);
    for (const m of parts) {
      m.isPickable = false; // set-aside stones are scenery, not targets
      m.position.y += 0.04;
    }
    root.scaling.setAll(0.72);
    fresh.push(slab, ...parts);
    return root;
  }

  private clearGraves(): void {
    for (const node of this.graves.values()) {
      node.getChildMeshes().forEach((m) => m.dispose());
      node.dispose();
    }
    this.graves.clear();
  }

  private createPiece(dto: PieceDto): PieceView {
    const pv = new PieceView(
      this.scene,
      dto.id,
      dto.owner,
      dto.value,
      dto.node,
      this.skinFor(dto.owner),
      this.nodeWorld(dto.node),
    );
    pv.root.parent = this.pieceRoot;
    this.pieces.set(dto.id, pv);
    return pv;
  }

  private removePiece(id: string): void {
    const pv = this.pieces.get(id);
    if (!pv) return;
    pv.dispose();
    this.pieces.delete(id);
  }

  clear(): void {
    for (const p of this.pieces.values()) p.dispose();
    this.pieces.clear();
    this.clearGraves();
    this.skins.clear();
    this.clearHints();
    this.clearTrail();
    this.select(null);
  }

  // ---- highlights ----

  select(node: string | null): void {
    if (!this.selectionRing) return;
    if (!node || !this.nodePos.has(node)) {
      this.selectionRing.setEnabled(false);
      return;
    }
    const p = this.nodeWorld(node);
    this.selectionRing.position.set(p.x, 0.05, p.z);
    this.selectionRing.setEnabled(true);
  }

  showHints(options: MoveOption[]): void {
    this.clearHints();
    const base = mat(this.scene, "hint", C.hint, { emissive: C.hint.scale(0.75), alpha: 0.85 });
    // A strike reads as a threat, not an invitation: red, and a wide ring that
    // encircles the stone about to be taken rather than hiding under it.
    const strike = mat(this.scene, "strike", C.strike, { emissive: C.strike, unlit: true });
    for (const o of options) {
      if (!this.nodePos.has(o.to)) continue;
      const p = this.nodeWorld(o.to);
      const capture = !!o.captures;
      const d = capture
        ? ring(this.scene, "strike", 0.29, 0.43, strike)
        : disc(this.scene, "hint", 0.24, 0.024, base);
      d.position.set(p.x, capture ? 0.055 : 0.03, p.z);
      d.metadata = { hintNode: o.to, capture };
      // The bloom washes strong colours out to white, which is exactly what a
      // warning marker must not be. Empty fields keep their glow; strikes are
      // excluded so the red actually reads as red.
      if (capture) this.glow?.addExcludedMesh(d);
      this.hints.set(o.to, d);
      if (capture) this.captureNodes.add(o.to);
    }
  }

  /** Is this field a strike? Used to colour the emphasis while dragging. */
  isCapture(node: string): boolean {
    return this.captureNodes.has(node);
  }

  clearHints(): void {
    for (const m of this.hints.values()) m.dispose();
    this.hints.clear();
    this.captureNodes.clear();
  }

  /** Swells the destination the held stone is hovering over. */
  emphasize(node: string | null): void {
    for (const [id, mesh] of this.hints) {
      const target = id === node ? 1.55 : 1;
      mesh.scaling.x += (target - mesh.scaling.x) * 0.3;
      mesh.scaling.z = mesh.scaling.x;
    }
  }

  private animateHints(): void {
    if (!this.hints.size && !this.selectionRing?.isEnabled()) return;
    this.pulse += this.scene.getEngine().getDeltaTime() / 1000;
    const k = 0.65 + 0.35 * Math.sin(this.pulse * 4.4);
    for (const [id, mesh] of this.hints) {
      const capture = this.captureNodes.has(id);
      const m = mesh.material as ReturnType<typeof mat>;
      // Unlit strikes carry their colour in the emissive alone, so keep them
      // near full brightness or the pulse dims them into the board.
      m.emissiveColor = capture ? C.strike.scale(0.8 + 0.2 * k) : C.hint.scale(0.45 + 0.5 * k);
      // Strike rings ride a little higher so they clear the stone they circle.
      mesh.position.y = (capture ? 0.055 : 0.03) + k * 0.012;
    }
    if (this.selectionRing?.isEnabled()) this.selectionRing.rotation.y += 0.012;
  }

  showTrail(path: string[]): void {
    this.clearTrail();
    const m = mat(this.scene, "trail", C.goldPale, { emissive: C.goldPale.scale(0.5), alpha: 0.8 });
    for (const id of path) {
      if (!this.nodePos.has(id)) continue;
      const p = this.nodeWorld(id);
      const dot = disc(this.scene, "trail", 0.075, 0.016, m);
      dot.position.set(p.x, 0.026, p.z);
      dot.isPickable = false;
      this.trail.push(dot);
    }
  }

  clearTrail(): void {
    this.trail.forEach((m) => m.dispose());
    this.trail = [];
  }
}

export type { PieceView };
export { PIECE_H };
