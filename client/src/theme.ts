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

export const C = {
  cloth: Color3.FromHexString("#2A2624"),
  clothEdge: Color3.FromHexString("#1A1614"),
  teal: Color3.FromHexString("#6EC6BB"),
  gold: Color3.FromHexString("#D9A83A"),
  goldDeep: Color3.FromHexString("#8A691F"),
  goldPale: Color3.FromHexString("#F3D27A"),
  node: Color3.FromHexString("#49555C"),
  hint: Color3.FromHexString("#7FD4C9"),
};

export function mat(
  scene: Scene,
  name: string,
  diffuse: Color3,
  opts: { emissive?: Color3; specular?: Color3; alpha?: number; roughness?: number } = {},
): StandardMaterial {
  const m = new StandardMaterial(name, scene);
  m.diffuseColor = diffuse;
  m.emissiveColor = opts.emissive ?? Color3.Black();
  m.specularColor = opts.specular ?? new Color3(0.12, 0.12, 0.12);
  m.specularPower = opts.roughness !== undefined ? 1 + (1 - opts.roughness) * 120 : 24;
  if (opts.alpha !== undefined) {
    m.alpha = opts.alpha;
    m.backFaceCulling = false;
  }
  return m;
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

// Hexagonal prism with corners pointing north/south, like the cloth board.
export function hexPrism(scene: Scene, name: string, radius: number, height: number, material: StandardMaterial): Mesh {
  const m = MeshBuilder.CreateCylinder(name, { diameter: radius * 2, height, tessellation: 6 }, scene);
  m.rotation.y = Math.PI / 2;
  m.material = material;
  return m;
}

export const clear = (hex: string): Color4 => Color4.FromHexString(hex.length === 7 ? hex + "ff" : hex);
