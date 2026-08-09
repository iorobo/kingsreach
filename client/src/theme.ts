import { Color3, Color4, DynamicTexture, MeshBuilder, StandardMaterial, Texture } from "./babylon";
import type { Mesh, Scene } from "./babylon";

// Tiled textures must repeat; DynamicTexture defaults to clamping, which
// smears the border pixels into wide bands once uScale/vScale go above 1.
function tiled(tex: DynamicTexture): DynamicTexture {
  tex.wrapU = Texture.WRAP_ADDRESSMODE;
  tex.wrapV = Texture.WRAP_ADDRESSMODE;
  return tex;
}

// Shared palette (matches the embroidered cloth from the physical set) and
// small factory helpers. Everything is generated at runtime — no assets.

/**
 * Board finishes. A preference, not a prize — every one of these is available
 * from the start, so the ids here have to match the "board" entries in the
 * server's catalog.
 */
export const BOARDS: Record<string, { cloth: string; edge: string }> = {
  slate: { cloth: "#2A2624", edge: "#1A1614" },
  walnut: { cloth: "#4A3325", edge: "#2E1E14" },
  ivory: { cloth: "#CFC4AC", edge: "#9C907A" },
  forest: { cloth: "#1F3A2C", edge: "#132419" },
  ink: { cloth: "#141317", edge: "#08080A" },
};

export const DEFAULT_BOARD = "slate";

/** The two colours of a board finish, falling back to the default. */
export function boardColours(id: string): { cloth: Color3; edge: Color3 } {
  const b = BOARDS[id] ?? BOARDS[DEFAULT_BOARD]!;
  return { cloth: Color3.FromHexString(b.cloth), edge: Color3.FromHexString(b.edge) };
}

export const C = {
  cloth: Color3.FromHexString("#2A2624"),
  clothEdge: Color3.FromHexString("#1A1614"),
  teal: Color3.FromHexString("#6EC6BB"),
  gold: Color3.FromHexString("#D9A83A"),
  goldDeep: Color3.FromHexString("#8A691F"),
  goldPale: Color3.FromHexString("#F3D27A"),
  node: Color3.FromHexString("#49555C"),
  hint: Color3.FromHexString("#7FD4C9"),
  // Strikes get their own colour. A capture is a different kind of move and
  // should not look like an empty field you happen to be able to reach.
  strike: Color3.FromHexString("#E05A45"),
};

export function mat(
  scene: Scene,
  name: string,
  diffuse: Color3,
  opts: {
    emissive?: Color3;
    specular?: Color3;
    alpha?: number;
    roughness?: number;
    /**
     * Ignore the scene lights and render the flat colour. Interface markers
     * want this: a lit surface blows out to white where the light hits it,
     * which is fine for a stone and useless for a warning.
     */
    unlit?: boolean;
  } = {},
): StandardMaterial {
  const m = new StandardMaterial(name, scene);
  m.diffuseColor = diffuse;
  m.emissiveColor = opts.emissive ?? Color3.Black();
  m.specularColor = opts.specular ?? new Color3(0.12, 0.12, 0.12);
  m.specularPower = opts.roughness !== undefined ? 1 + (1 - opts.roughness) * 120 : 24;
  if (opts.unlit) {
    m.disableLighting = true;
    m.specularColor = Color3.Black();
  }
  if (opts.alpha !== undefined) {
    m.alpha = opts.alpha;
    m.backFaceCulling = false;
  }
  return m;
}

/**
 * A soft round dot, for particles.
 *
 * Drawn rather than shipped, and specifically *not* the famous one-pixel
 * base64 GIF: that one is the transparent pixel every tracking image uses, so
 * a particle system given it renders a few hundred perfectly invisible sparks
 * while getActiveCount() cheerfully reports a hundred and seventy. Asked twice
 * now, hence a shared helper.
 */
export function softDot(scene: Scene): DynamicTexture {
  const size = 32;
  const tex = new DynamicTexture("spark", { width: size, height: size }, scene, false);
  const ctx = tex.getContext() as CanvasRenderingContext2D;
  const half = size / 2;
  const fade = ctx.createRadialGradient(half, half, 0, half, half, half);
  fade.addColorStop(0, "rgba(255,255,255,1)");
  fade.addColorStop(0.45, "rgba(255,255,255,0.8)");
  fade.addColorStop(1, "rgba(255,255,255,0)");
  ctx.fillStyle = fade;
  ctx.fillRect(0, 0, size, size);
  tex.update();
  tex.hasAlpha = true;
  return tex;
}

export function checkerTexture(scene: Scene, a: string, b: string): DynamicTexture {
  const size = 256;
  const tex = new DynamicTexture("checker", { width: size, height: size }, scene, false);
  const ctx = tex.getContext() as CanvasRenderingContext2D;
  const cell = size / 4;
  for (let y = 0; y < 4; y++) {
    for (let x = 0; x < 4; x++) {
      ctx.fillStyle = (x + y) % 2 === 0 ? a : b;
      ctx.fillRect(x * cell, y * cell, cell, cell);
    }
  }
  tex.update();
  return tiled(tex);
}

export function woodTexture(scene: Scene, base: string, grain: string): DynamicTexture {
  const size = 512;
  const tex = new DynamicTexture("wood", { width: size, height: size }, scene, false);
  const ctx = tex.getContext() as CanvasRenderingContext2D;
  ctx.fillStyle = base;
  ctx.fillRect(0, 0, size, size);
  ctx.strokeStyle = grain;
  ctx.lineWidth = 2;
  for (let i = 0; i < 42; i++) {
    const y = Math.random() * size;
    ctx.globalAlpha = 0.12 + Math.random() * 0.22;
    ctx.beginPath();
    ctx.moveTo(0, y);
    ctx.bezierCurveTo(size / 3, y + (Math.random() - 0.5) * 26, (2 * size) / 3, y + (Math.random() - 0.5) * 26, size, y);
    ctx.stroke();
  }
  ctx.globalAlpha = 1;
  tex.update();
  return tiled(tex);
}

// Flat disc lying in the XZ plane (a very short cylinder).
export function disc(
  scene: Scene,
  name: string,
  radius: number,
  height: number,
  material: StandardMaterial,
  parent?: Mesh,
): Mesh {
  const m = MeshBuilder.CreateCylinder(name, { diameter: radius * 2, height, tessellation: 40 }, scene);
  m.material = material;
  if (parent) m.parent = parent;
  return m;
}

// Flat annulus lying in the XZ plane. Used to encircle a stone rather than
// cover it: a strike marker has to sit *around* the piece being taken, or the
// player cannot see what they are about to capture.
export function ring(
  scene: Scene,
  name: string,
  innerRadius: number,
  outerRadius: number,
  material: StandardMaterial,
): Mesh {
  const m = MeshBuilder.CreateTorus(
    name,
    {
      diameter: innerRadius + outerRadius,
      thickness: outerRadius - innerRadius,
      tessellation: 44,
    },
    scene,
  );
  m.scaling.y = 0.5; // flatten it onto the board
  m.material = material;
  return m;
}

// Hexagonal prism with corners pointing north/south, like the cloth board.
export function hexPrism(scene: Scene, name: string, radius: number, height: number, material: StandardMaterial): Mesh {
  const m = MeshBuilder.CreateCylinder(name, { diameter: radius * 2, height, tessellation: 6 }, scene);
  m.rotation.y = Math.PI / 2;
  m.material = material;
  return m;
}

export const clear = (hex: string): Color4 => Color4.FromHexString(hex.length === 7 ? hex + "ff" : hex);
