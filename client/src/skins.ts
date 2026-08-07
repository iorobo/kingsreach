import { Color3, MeshBuilder, Vector3 } from "./babylon";
import type { Mesh, Scene } from "./babylon";
import { C, disc, mat } from "./theme";
import { seatInfo } from "./seats";
import type { Seat } from "./api";

// Unlockable piece skins. A seat supplies the colour (see seats.ts) and the
// skin decides how that colour is rendered — matte ceramic, polished metal,
// glowing gem, weathered stone. That split is what lets every skin work for
// two, three or four players without a palette per combination.

export const PIECE_H = 0.15;

function pipSpots(value: number): [number, number][] {
  if (value === 1) return [[0, 0]];
  if (value === 2) return [[-0.11, 0], [0.11, 0]];
  return [[0, 0.11], [-0.1, -0.07], [0.1, -0.07]];
}

/** Light hues need dark pips and vice versa, or the value becomes unreadable. */
function luminance(c: Color3): number {
  return 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b;
}

function contrastTo(c: Color3): Color3 {
  return luminance(c) > 0.42 ? Color3.FromHexString("#26201A") : Color3.FromHexString("#EFE2C0");
}

interface SkinStyle {
  body: () => ReturnType<typeof mat>;
  pip: () => ReturnType<typeof mat>;
  crown?: () => ReturnType<typeof mat>;
  shape?: "disc" | "gem" | "stone";
}

function styleFor(scene: Scene, skinId: string, seat: Seat): SkinStyle {
  const hue = seatInfo(seat).hue;
  const ink = contrastTo(hue);

  switch (skinId) {
    case "royal":
      return {
        body: () =>
          mat(scene, "b", hue.scale(1.05), {
            specular: new Color3(0.95, 0.9, 0.7),
            roughness: 0.1,
          }),
        pip: () => mat(scene, "p", ink, { roughness: 0.25 }),
        crown: () =>
          mat(scene, "c", Color3.Lerp(hue, Color3.White(), 0.55), {
            emissive: hue.scale(0.22),
            specular: new Color3(1, 0.96, 0.82),
            roughness: 0.06,
          }),
      };
    case "crystal": {
      // The gem is a pale, glowing version of the seat's hue, so pips *of that
      // hue* vanish into it — which is exactly what they used to do, leaving
      // the piece's value unreadable. They take the contrast colour like every
      // other skin, unlit so the glow cannot wash them out again.
      const gemBody = Color3.Lerp(hue, Color3.White(), 0.35);
      return {
        shape: "gem",
        body: () =>
          mat(scene, "b", gemBody, {
            emissive: hue.scale(0.45),
            specular: new Color3(1, 1, 1),
            alpha: 0.82,
            roughness: 0.05,
          }),
        pip: () => mat(scene, "p", contrastTo(gemBody), { unlit: true }),
      };
    }
    case "rune":
      return {
        shape: "stone",
        // The stone stays grey whoever you are; the carved runes carry the colour.
        body: () => mat(scene, "b", Color3.Lerp(hue, Color3.FromHexString("#54544F"), 0.72), { roughness: 0.95 }),
        pip: () => mat(scene, "p", hue.scale(0.3), { emissive: hue.scale(1.3) }),
      };
    default:
      return {
        body: () => mat(scene, "b", hue, { roughness: 0.72 }),
        pip: () => mat(scene, "p", ink, { roughness: 0.45 }),
        crown: () =>
          mat(scene, "c", Color3.Lerp(hue, C.gold, 0.65), {
            specular: new Color3(0.6, 0.5, 0.25),
            roughness: 0.3,
          }),
      };
  }
}

/**
 * Builds the meshes for one piece under `root` and returns them all.
 *
 * `rank` is the owner's place in the hall of champions, 0 for everyone else.
 * The top three get a taller, gilded crown on their king — the whole point of
 * a leaderboard is that it shows at the table, not only on a screen you have
 * to go and open.
 */
export function buildPiece(
  scene: Scene,
  root: Mesh,
  skinId: string,
  seat: Seat,
  value: number,
  rank = 0,
): Mesh[] {
  const st = styleFor(scene, skinId, seat);
  const bodyMat = st.body();
  const pipMat = st.pip();
  const parts: Mesh[] = [];
  let pipY = PIECE_H;

  if (st.shape === "gem") {
    const gem = MeshBuilder.CreatePolyhedron("gem", { type: 1, size: 0.24 }, scene);
    gem.material = bodyMat;
    gem.parent = root;
    gem.position.y = 0.24;
    parts.push(gem);
    pipY = 0.42;
  } else if (st.shape === "stone") {
    const rock = MeshBuilder.CreateBox("rock", { width: 0.52, height: PIECE_H, depth: 0.52 }, scene);
    rock.material = bodyMat;
    rock.parent = root;
    rock.position.y = PIECE_H / 2;
    rock.rotation.y = 0.26;
    parts.push(rock);
    const cap = MeshBuilder.CreateBox("rock2", { width: 0.4, height: PIECE_H * 0.55, depth: 0.4 }, scene);
    cap.material = bodyMat;
    cap.parent = root;
    cap.position.y = PIECE_H * 0.92;
    cap.rotation.y = 0.7;
    parts.push(cap);
    pipY = PIECE_H * 1.2;
  } else {
    const body = disc(scene, "body", 0.33, PIECE_H, bodyMat, root);
    body.position.y = PIECE_H / 2;
    parts.push(body);
    if (skinId === "royal") {
      const rim = disc(scene, "rim", 0.35, 0.02, bodyMat, root);
      rim.position.y = PIECE_H - 0.01;
      parts.push(rim);
    }
  }

  for (const [px, pz] of pipSpots(value)) {
    const pip = MeshBuilder.CreateSphere("pip", { diameter: 0.095, segments: 10 }, scene);
    pip.material = pipMat;
    pip.parent = root;
    pip.position = new Vector3(px, pipY + 0.033, pz);
    parts.push(pip);
  }

  if (value === 1) {
    const crowned = rank >= 1 && rank <= 3;
    const crownMat = crowned ? championCrown(scene, rank) : st.crown ? st.crown() : pipMat;
    const band = disc(scene, "crown", crowned ? 0.16 : 0.13, crowned ? 0.045 : 0.03, crownMat, root);
    band.position.y = pipY + 0.08;
    parts.push(band);

    if (crowned) {
      // Points round the band, one more for each place up the board, so first
      // and third are told apart at a glance rather than by counting pixels.
      const points = 6 - rank;
      for (let i = 0; i < points; i++) {
        const spike = MeshBuilder.CreateCylinder(
          "crown-point",
          { diameterBottom: 0.05, diameterTop: 0, height: 0.13, tessellation: 6 },
          scene,
        );
        spike.material = crownMat;
        spike.parent = root;
        const a = (i / points) * Math.PI * 2;
        spike.position = new Vector3(Math.cos(a) * 0.13, pipY + 0.16, Math.sin(a) * 0.13);
        parts.push(spike);
      }
    }

    const tip = MeshBuilder.CreateSphere("crown-tip", { diameter: crowned ? 0.13 : 0.1, segments: 10 }, scene);
    tip.material = crownMat;
    tip.parent = root;
    tip.position.y = pipY + (crowned ? 0.22 : 0.17);
    parts.push(tip);
  }

  return parts;
}

/** Gold for a champion, brightest at the top of the board. */
function championCrown(scene: Scene, rank: number) {
  const shine = [0.55, 0.38, 0.24][rank - 1] ?? 0.24;
  return mat(scene, "crown-champion", C.gold, {
    emissive: C.goldPale.scale(shine),
    specular: new Color3(1, 0.95, 0.75),
    roughness: 0.05,
  });
}
