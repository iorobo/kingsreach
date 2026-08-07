import {
  Color3,
  Color4,
  DirectionalLight,
  HemisphericLight,
  MeshBuilder,
  PointLight,
  Scene,
  ShadowGenerator,
  TransformNode,
  Vector3,
} from "./babylon";
import type { Mesh, Texture } from "./babylon";
import type { LoadedModel, ModelPlacement } from "./gltf";
import { checkerTexture, mat, woodTexture } from "./theme";

// Unlockable 3D surroundings. The board's cloth always rests with its top
// face at y = 0; an environment builds the world around and beneath it.
// Ids match the server catalog (picnic / fair / store / cafe).

export interface BuiltEnvironment {
  root: TransformNode;
  shadows: ShadowGenerator | null;
  dispose(): void;
}

// Pulls in the glTF loader only when a model-based environment is used.
async function loadModel(
  scene: Scene,
  parent: TransformNode,
  file: string,
  opts: ModelPlacement,
  cancelled: () => boolean,
): Promise<LoadedModel | null> {
  const { loadGlb } = await import("./gltf");
  if (cancelled()) return null;
  return loadGlb(scene, parent, file, opts, cancelled);
}

// Deterministic pseudo-random so a given environment always looks the same.
function rng(seed: number): () => number {
  let s = seed;
  return () => {
    s = (s * 1664525 + 1013904223) % 4294967296;
    return s / 4294967296;
  };
}

function sun(scene: Scene, root: TransformNode, dir: Vector3, color: Color3, intensity: number): ShadowGenerator {
  const light = new DirectionalLight("sun", dir, scene);
  light.parent = root;
  light.diffuse = color;
  light.intensity = intensity;
  light.position = dir.scale(-30);
  const shadows = new ShadowGenerator(1024, light);
  shadows.useBlurExponentialShadowMap = true;
  shadows.blurKernel = 24;
  shadows.darkness = 0.45;
  return shadows;
}

function ambient(scene: Scene, root: TransformNode, color: Color3, intensity: number): void {
  const h = new HemisphericLight("ambient", new Vector3(0, 1, 0), scene);
  h.parent = root;
  h.diffuse = color;
  h.groundColor = color.scale(0.45);
  h.intensity = intensity;
}

function lamp(scene: Scene, root: TransformNode, pos: Vector3, color: Color3, intensity: number, range: number): void {
  const l = new PointLight("lamp", pos, scene);
  l.parent = root;
  l.diffuse = color;
  l.intensity = intensity;
  l.range = range;
}

// A round table for the indoor scenes: top just below the cloth.
function table(scene: Scene, root: TransformNode, radius: number, floorY: number, topTex: Texture, legHex: string): Mesh[] {
  const topMat = mat(scene, "table-top", new Color3(1, 1, 1), { roughness: 0.45 });
  topMat.diffuseTexture = topTex;
  const legMat = mat(scene, "table-leg", Color3.FromHexString(legHex), { roughness: 0.4 });

  const top = MeshBuilder.CreateCylinder("table", { diameter: radius * 2, height: 0.22, tessellation: 48 }, scene);
  top.material = topMat;
  top.parent = root;
  top.position.y = -0.29;
  top.receiveShadows = true;

  const legLen = -0.4 - floorY;
  const stem = MeshBuilder.CreateCylinder("stem", { diameter: 0.75, height: legLen, tessellation: 20 }, scene);
  stem.material = legMat;
  stem.parent = root;
  stem.position.y = -0.4 - legLen / 2;

  const foot = MeshBuilder.CreateCylinder("foot", { diameter: radius * 1.2, height: 0.14, tessellation: 32 }, scene);
  foot.material = legMat;
  foot.parent = root;
  foot.position.y = floorY + 0.07;

  return [top, stem, foot];
}

function ground(scene: Scene, root: TransformNode, y: number, size: number, material: ReturnType<typeof mat>): Mesh {
  const g = MeshBuilder.CreateGround("ground", { width: size, height: size }, scene);
  g.material = material;
  g.parent = root;
  g.position.y = y;
  g.receiveShadows = true;
  return g;
}

function walls(scene: Scene, root: TransformNode, hex: string, floorY: number, half: number, height: number): void {
  const m = mat(scene, "wall", Color3.FromHexString(hex), { roughness: 0.9 });
  const specs: [number, number, number, number][] = [
    [0, half, half * 2, 0.4],
    [0, -half, half * 2, 0.4],
    [half, 0, 0.4, half * 2],
    [-half, 0, 0.4, half * 2],
  ];
  for (const [x, z, w, d] of specs) {
    const wall = MeshBuilder.CreateBox("wall", { width: w, height, depth: d }, scene);
    wall.material = m;
    wall.parent = root;
    wall.position.set(x, floorY + height / 2, z);
  }
}

// ---- 1. Picnic Meadow (default) ----

function buildPicnic(scene: Scene, root: TransformNode): ShadowGenerator {
  scene.clearColor = Color4.FromHexString("#9CC8E8ff");
  scene.fogMode = Scene.FOGMODE_EXP2;
  scene.fogColor = Color3.FromHexString("#B8D4E8");
  scene.fogDensity = 0.008;
  ambient(scene, root, Color3.FromHexString("#DCE8F0"), 0.75);
  const shadows = sun(scene, root, new Vector3(-0.45, -0.85, 0.3), Color3.FromHexString("#FFF3DC"), 1.15);

  ground(scene, root, -0.2, 140, mat(scene, "grass", Color3.FromHexString("#5E8C4A"), { roughness: 0.95 }));

  const trunkMat = mat(scene, "trunk", Color3.FromHexString("#6B4A2E"), { roughness: 0.95 });
  const leafA = mat(scene, "leafA", Color3.FromHexString("#4A7A38"), { roughness: 0.95 });
  const leafB = mat(scene, "leafB", Color3.FromHexString("#5E8C42"), { roughness: 0.95 });
  const spots: [number, number][] = [[-17, 10], [15, 14], [19, -9], [-14, -15], [3, 21], [-22, -2]];
  const rand = rng(11);
  spots.forEach(([x, z], i) => {
    const h = 3.6 + rand() * 1.8;
    const trunk = MeshBuilder.CreateCylinder("trunk", { diameter: 0.85, height: h, tessellation: 10 }, scene);
    trunk.material = trunkMat;
    trunk.parent = root;
    trunk.position.set(x, h / 2 - 0.2, z);
    const crown = MeshBuilder.CreateSphere("leaves", { diameter: 4, segments: 8 }, scene);
    crown.material = i % 2 ? leafA : leafB;
    crown.parent = root;
    crown.position.set(x, h + 1, z);
    crown.scaling.y = 0.82;
    const puff = MeshBuilder.CreateSphere("leaves2", { diameter: 2.6, segments: 8 }, scene);
    puff.material = leafB;
    puff.parent = root;
    puff.position.set(x + 1, h + 0.2, z + 0.6);
  });
  return shadows;
}

// ---- 2. Medieval Fair ----

function buildFair(scene: Scene, root: TransformNode): ShadowGenerator {
  scene.clearColor = Color4.FromHexString("#5A3A50ff");
  scene.fogMode = Scene.FOGMODE_EXP2;
  scene.fogColor = Color3.FromHexString("#6E4A56");
  scene.fogDensity = 0.02;
  ambient(scene, root, Color3.FromHexString("#8A6E7A"), 0.5);
  const shadows = sun(scene, root, new Vector3(-0.8, -0.42, 0.35), Color3.FromHexString("#FFB870"), 0.95);
  lamp(scene, root, new Vector3(7, 3.4, 6), Color3.FromHexString("#FFAA55"), 0.8, 18);
  lamp(scene, root, new Vector3(-8, 3.4, -5), Color3.FromHexString("#FFAA55"), 0.8, 18);

  ground(scene, root, -0.2, 140, mat(scene, "dirt", Color3.FromHexString("#7A6244"), { roughness: 0.98 }));
  const rug = MeshBuilder.CreateGround("rug", { width: 13, height: 13 }, scene);
  rug.material = mat(scene, "rug", Color3.FromHexString("#7A2E2E"), { roughness: 0.9 });
  rug.parent = root;
  rug.position.y = -0.18;
  rug.receiveShadows = true;

  const poleMat = mat(scene, "pole", Color3.FromHexString("#5E4630"), { roughness: 0.9 });
  const tentCols = ["#A63A3A", "#3A5AA6", "#A6883A", "#4A7A4A"];
  const tents: [number, number][] = [[-15, 9], [14, 11], [16, -10], [-13, -13]];
  tents.forEach(([x, z], i) => {
    const pole = MeshBuilder.CreateCylinder("tent-pole", { diameter: 0.4, height: 3.8, tessellation: 8 }, scene);
    pole.material = poleMat;
    pole.parent = root;
    pole.position.set(x, 1.7, z);
    const roof = MeshBuilder.CreateCylinder(
      "tent-roof",
      { diameterTop: 0, diameterBottom: 8.4, height: 2.8, tessellation: 10 },
      scene,
    );
    roof.material = mat(scene, "tent", Color3.FromHexString(tentCols[i]!), { roughness: 0.85 });
    roof.parent = root;
    roof.position.set(x, 4.4, z);
    const crate = MeshBuilder.CreateBox("crate", { size: 1.3 }, scene);
    crate.material = poleMat;
    crate.parent = root;
    crate.position.set(x + 2.8, 0.45, z - 1.9);
    crate.rotation.y = i * 0.7;
  });

  // Bunting strung across the tourney ground.
  const flagCols = ["#E8C34A", "#C3453F", "#3F63C3"];
  for (let i = 0; i < 11; i++) {
    const t = i / 10;
    const flag = MeshBuilder.CreateBox("flag", { width: 0.6, height: 0.8, depth: 0.04 }, scene);
    flag.material = mat(scene, "flag", Color3.FromHexString(flagCols[i % 3]!), { roughness: 0.8 });
    flag.parent = root;
    flag.position.set(-9 + t * 18, 5.6 - Math.sin(t * Math.PI) * 1.1, 9.5);
    flag.rotation.z = Math.PI / 4;
  }
  return shadows;
}

// ---- 3. Boardgame Store ----

function buildStore(scene: Scene, root: TransformNode): ShadowGenerator {
  scene.clearColor = Color4.FromHexString("#241C16ff");
  scene.fogMode = Scene.FOGMODE_EXP2;
  scene.fogColor = Color3.FromHexString("#241C16");
  scene.fogDensity = 0.022;
  ambient(scene, root, Color3.FromHexString("#8A7660"), 0.55);
  const shadows = sun(scene, root, new Vector3(-0.3, -0.92, 0.25), Color3.FromHexString("#FFE8C8"), 0.75);
  lamp(scene, root, new Vector3(0, 4.6, 0), Color3.FromHexString("#FFD9A0"), 1.1, 22);
  lamp(scene, root, new Vector3(-10, 3.6, 7), Color3.FromHexString("#FFC880"), 0.6, 16);

  const floorY = -3.6;
  const floorMat = mat(scene, "floor", new Color3(1, 1, 1), { roughness: 0.7 });
  const ft = woodTexture(scene, "#6E4E32", "#4A3320");
  ft.uScale = 8;
  ft.vScale = 8;
  floorMat.diffuseTexture = ft;
  ground(scene, root, floorY, 70, floorMat);
  walls(scene, root, "#3E3226", floorY, 26, 11);

  const tableTex = woodTexture(scene, "#8A6542", "#5E4426");
  tableTex.uScale = 2;
  tableTex.vScale = 2;
  table(scene, root, 6.8, floorY, tableTex, "#4A3722");

  const shelfMat = mat(scene, "shelf", Color3.FromHexString("#5E4630"), { roughness: 0.85 });
  const rand = rng(29);
  const shelves: [number, number, number][] = [
    [-16, 22, 0],
    [2, 24, 0],
    [18, 22, 0],
    [-24, -12, Math.PI / 2],
    [24, -14, Math.PI / 2],
  ];
  for (const [sx, sz, rot] of shelves) {
    for (let lvl = 0; lvl < 4; lvl++) {
      const y = floorY + 1.1 + lvl * 1.3;
      const plank = MeshBuilder.CreateBox("shelf", { width: 8, height: 0.14, depth: 1.5 }, scene);
      plank.material = shelfMat;
      plank.parent = root;
      plank.position.set(sx, y, sz);
      plank.rotation.y = rot;
      for (let b = 0; b < 7; b++) {
        const w = 0.55 + rand() * 0.45;
        const h = 0.55 + rand() * 0.5;
        const box = MeshBuilder.CreateBox("gamebox", { width: w, height: h, depth: 1.1 }, scene);
        box.material = mat(scene, "gb", Color3.FromHSV(rand() * 360, 0.55 + rand() * 0.35, 0.55 + rand() * 0.4), {
          roughness: 0.6,
        });
        box.parent = root;
        const off = -3.3 + b * 0.95;
        box.position.set(sx + (rot ? 0 : off), y + h / 2 + 0.08, sz + (rot ? off : 0));
        box.rotation.y = rot;
      }
    }
  }
  return shadows;
}

// ---- 4. Streetside Café ----

/**
 * Fades scenery that comes between the camera and the board.
 *
 * Placing props "out of the sightline" only works if you know where players
 * sit, and this game seats two, three or four of them at six possible corners
 * — and lets everyone orbit freely on top of that. Rather than guess an angle
 * that is clear for every combination, anything that ends up in front of the
 * board simply gets out of the way while it is there.
 */
function fadeWhenInTheWay(scene: Scene, meshes: Mesh[]): void {
  // The board is a flat disc. Two tests are tempting and both wrong: the line
  // to its *centre* misses a lamp hanging in front of the far edge, and a
  // sphere around it hides the lamps almost always, because a sphere that
  // wide also reaches up to where they hang.
  //
  // So ask the question directly: follow the ray from the eye through the
  // prop, and see whether it lands on the board beyond it. If it does, the
  // prop is between the player and a piece of board they want to see.
  const BOARD_RADIUS = 5.6;
  const PROP_RADIUS = 0.85;

  for (const m of meshes) {
    m.isPickable = false;
    m.visibility = 1;
  }
  scene.onBeforeRenderObservable.add(() => {
    const camera = scene.activeCamera;
    if (!camera) return;
    const eye = camera.globalPosition;

    for (const m of meshes) {
      const at = m.getAbsolutePosition();
      const toProp = at.subtract(eye);
      const dist = toProp.length();
      let hide = false;
      if (dist > 0.01 && toProp.y < 0) {
        const dir = toProp.scale(1 / dist);
        const t = -eye.y / dir.y; // where the ray meets the board plane
        if (t > dist) {
          const hit = eye.add(dir.scale(t));
          hide = Math.hypot(hit.x, hit.z) < BOARD_RADIUS + PROP_RADIUS;
        }
      }
      const want = hide ? 0.1 : 1;
      m.visibility += (want - m.visibility) * 0.18; // ease, so it never blinks
    }
  });
}

function buildCafe(scene: Scene, root: TransformNode): ShadowGenerator {
  scene.clearColor = Color4.FromHexString("#141218ff");
  scene.fogMode = Scene.FOGMODE_EXP2;
  scene.fogColor = Color3.FromHexString("#141218");
  scene.fogDensity = 0.025;
  ambient(scene, root, Color3.FromHexString("#6E6A78"), 0.4);
  const shadows = sun(scene, root, new Vector3(0.15, -0.95, -0.2), Color3.FromHexString("#FFE0B0"), 0.85);
  // These sat at z = ±4.4 on the reasoning that "players look along the X
  // axis" — true when the game seated two people, west and east. At three or
  // four the seats are on the diagonals and look straight through them.
  // Nudged outward, but the real fix is `fadeWhenInTheWay` below: pendants
  // belong over the table, so rather than banish them somewhere they can
  // never be in shot, they step aside while they are.
  lamp(scene, root, new Vector3(0, 3.4, -5.4), Color3.FromHexString("#FFD9A0"), 0.95, 15);
  lamp(scene, root, new Vector3(0, 3.4, 5.4), Color3.FromHexString("#FFD9A0"), 0.95, 15);
  lamp(scene, root, new Vector3(12, 3.2, -9), Color3.FromHexString("#FFC880"), 0.5, 14);

  const floorY = -3.6;
  const floorMat = mat(scene, "floor", new Color3(1, 1, 1), { roughness: 0.35 });
  const ct = checkerTexture(scene, "#2E2A26", "#C8BFAE");
  ct.uScale = 10;
  ct.vScale = 10;
  floorMat.diffuseTexture = ct;
  ground(scene, root, floorY, 70, floorMat);
  walls(scene, root, "#2A2530", floorY, 26, 11);

  const tableTex = woodTexture(scene, "#6E4632", "#4A2E1E");
  table(scene, root, 6.2, floorY, tableTex, "#2A2622");

  // Pendant lamps over the board, grouped so they can get out of the way.
  const shadeMat = mat(scene, "shade", Color3.FromHexString("#8A3A2E"), { roughness: 0.4 });
  const bulbMat = mat(scene, "bulb", Color3.FromHexString("#FFE8B8"), { emissive: Color3.FromHexString("#FFDCA0") });
  for (const z of [-5.4, 5.4]) {
    const pendant = new TransformNode("pendant", scene);
    pendant.parent = root;
    const cord = MeshBuilder.CreateCylinder("cord", { diameter: 0.05, height: 2.2, tessellation: 6 }, scene);
    cord.material = mat(scene, "cord", Color3.FromHexString("#1A1A1A"), { roughness: 1 });
    cord.parent = pendant;
    cord.position.set(0, 4.8, z);
    const shade = MeshBuilder.CreateCylinder(
      "shade",
      { diameterTop: 0.25, diameterBottom: 1.5, height: 0.9, tessellation: 20 },
      scene,
    );
    shade.material = shadeMat;
    shade.parent = pendant;
    shade.position.set(0, 3.7, z);
    const bulb = MeshBuilder.CreateSphere("bulb", { diameter: 0.34, segments: 10 }, scene);
    bulb.material = bulbMat;
    bulb.parent = pendant;
    bulb.position.set(0, 3.35, z);
    fadeWhenInTheWay(scene, [cord, shade, bulb]);
  }

  const woodDark = mat(scene, "cafe-wood", Color3.FromHexString("#4A362A"), { roughness: 0.5 });
  const cupMat = mat(scene, "cup", Color3.FromHexString("#E8E0D0"), { roughness: 0.3 });
  const spots: [number, number][] = [[-14, 10], [14, 9], [13, -13], [-13, -12]];
  for (const [x, z] of spots) {
    const top = MeshBuilder.CreateCylinder("cafe-table", { diameter: 3.4, height: 0.14, tessellation: 24 }, scene);
    top.material = woodDark;
    top.parent = root;
    top.position.set(x, floorY + 2.4, z);
    const leg = MeshBuilder.CreateCylinder("cafe-leg", { diameter: 0.3, height: 2.4, tessellation: 10 }, scene);
    leg.material = woodDark;
    leg.parent = root;
    leg.position.set(x, floorY + 1.2, z);
    const cup = MeshBuilder.CreateCylinder("cup", { diameter: 0.36, height: 0.24, tessellation: 14 }, scene);
    cup.material = cupMat;
    cup.parent = root;
    cup.position.set(x + 0.8, floorY + 2.6, z + 0.4);
  }
  return shadows;
}

// ---- 5. Bombsite B ----
// The board stands on the big X-braced wooden crate on bombsite B, the one
// beside the green ammo stack by the "B" spray. The map is a Z-up Source
// export in source units, scaled until that 96-unit crate top is a shade
// wider than the board (14.6 vs 11.2), then anchored by the centre of that
// face — expressed in the model's own upright, unscaled frame, so the scale
// and the anchor stay independent of each other.
const DUST_WIDTH = 807;
const DUST_ANCHOR = new Vector3(1808, 96, -2384);

function buildDust2(scene: Scene, root: TransformNode, ctx: LoadContext): ShadowGenerator {
  scene.clearColor = Color4.FromHexString("#D8C79Cff");
  scene.fogMode = Scene.FOGMODE_EXP2;
  scene.fogColor = Color3.FromHexString("#D2BF95");
  scene.fogDensity = 0.0035;
  ambient(scene, root, Color3.FromHexString("#EADFC4"), 1);
  const shadows = sun(scene, root, new Vector3(-0.45, -0.78, 0.44), Color3.FromHexString("#FFF1CE"), 1.1);

  void loadModel(
    scene,
    root,
    "de-dust2.glb",
    // The crate lid lands just under the board's underside (its seam reaches
    // y = -0.215); flush with y = 0 would swallow the cloth entirely.
    { targetWidth: DUST_WIDTH, position: new Vector3(0, -0.28, 0), anchor: DUST_ANCHOR, zUp: true },
    ctx.cancelled,
  )
    .then((model) => {
      if (!model) return;
      // The map only receives shadows: adding a level this size as a caster
      // would stretch the shadow map across it and erase the pieces' own.
      for (const m of model.meshes) m.receiveShadows = true;
      ctx.onLoaded?.();
    })
    .catch((e) => console.warn("dust2 model failed to load:", e));

  return shadows;
}

interface LoadContext {
  cancelled: () => boolean;
  onLoaded?: () => void;
}

const builders: Record<string, (scene: Scene, root: TransformNode, ctx: LoadContext) => ShadowGenerator> = {
  picnic: buildPicnic,
  fair: buildFair,
  dust2: buildDust2,
  store: buildStore,
  cafe: buildCafe,
};

export function buildEnvironment(scene: Scene, envId: string, onLoaded?: () => void): BuiltEnvironment {
  const root = new TransformNode("environment-" + envId, scene);
  let disposed = false;
  const build = builders[envId] ?? buildPicnic;
  const shadows = build(scene, root, { cancelled: () => disposed, onLoaded });
  return {
    root,
    shadows,
    dispose() {
      disposed = true;
      shadows?.dispose();
      root.getChildMeshes().forEach((m) => m.dispose());
      root.dispose();
    },
  };
}
