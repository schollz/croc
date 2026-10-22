// Wire and scalar compatibility with the pinned github.com/schollz/pake/v3.
import { p256, p384, p521 } from "@noble/curves/nist.js";
import { ed25519 } from "@noble/curves/ed25519.js";
import { weierstrass } from "@noble/curves/abstract/weierstrass.js";
import type {
  CurvePoint,
  CurvePointCons,
} from "@noble/curves/abstract/curve.js";
import { parse, stringify } from "lossless-json";
import { concatBytes, randomBytes, textEncoder } from "./bytes";
import { frame, identities, sha256 } from "./key";
interface Point extends CurvePoint<bigint, Point> {}
type Group = CurvePointCons<Point>;
const siec = weierstrass({
  p: 28948022309329048855892746252183396360603931420023084536990047309120118726721n,
  n: 28948022309329048855892746252183396360263649053102146073526672701688283398081n,
  h: 1n,
  a: 0n,
  b: 19n,
  Gx: 5n,
  Gy: 12n,
});
const groups: Record<string, Group> = {
  p256: p256.Point,
  p384: p384.Point,
  p521: p521.Point,
  siec,
  ed25519: ed25519.Point,
};
// Public, fixed masking points from the pinned Go package, in Go's representation.
const fixed: Record<string, bigint[]> = {
  p521: [
    793136080485469241208656611513609866400481671852n,
    4032821203812196944795502391345776760852202059010382256134592838722123385325802540879231526503456158741518531456199762365161310489884151533417829496019094620n,
    1086685267857089638167386722555472967068468061489n,
    5010916268086655347194655708160715195931018676225831839835602465999566066450501167246678404591906342753230577187831311039273858772817427392089150297708931207n,
  ],
  p256: [
    793136080485469241208656611513609866400481671852n,
    59748757929350367369315811184980635230185250460108398961713395032485227207304n,
    1086685267857089638167386722555472967068468061489n,
    9157340230202296554417312816309453883742349874205386245733062928888341584123n,
  ],
  p384: [
    793136080485469241208656611513609866400481671852n,
    7854890799382392388170852325516804266858248936799429260403044177981810983054351714387874260245230531084533936948596n,
    1086685267857089638167386722555472967068468061489n,
    21898206562669911998235297167979083576432197282633635629145270958059347586763418294901448537278960988843108277491616n,
  ],
  siec: [
    793136080485469241208656611513609866400481671853n,
    18458907634222644275952014841865282643645472623913459400556233196838128612339n,
    1086685267857089638167386722555472967068468061489n,
    19593504966619549205903364028255899745298716108914514072669075231742699650911n,
  ],
  ed25519: [
    41821174510521985817056358996007359290163947216650231187782646151092828043509n,
    0n,
    1456941786990260824647297143563623381366314063537015067473110401627488371271n,
    0n,
  ],
};
export function integerBytes(value: bigint, length?: number) {
  if (value === 0n && length === undefined) return new Uint8Array();
  let hex = value.toString(16);
  if (hex.length % 2) hex = "0" + hex;
  const bytes = Uint8Array.from(hex.match(/../g) ?? [], (x) => parseInt(x, 16));
  if (length === undefined) return bytes;
  if (bytes.length > length) throw new Error("PAKE coordinate too large");
  const result = new Uint8Array(length);
  result.set(bytes, length - bytes.length);
  return result;
}
function integer(bytes: Uint8Array) {
  let result = 0n;
  for (const byte of bytes) result = result * 256n + BigInt(byte);
  return result;
}
export class Pake {
  private group: Group;
  private u: Point;
  private v: Point;
  private x?: Point;
  private y?: Point;
  private secret?: Uint8Array;
  private ids?: Uint8Array[];
  constructor(
    private password: Uint8Array,
    private role: 0 | 1,
    private curve: string,
    purpose?: string,
    room?: string,
  ) {
    if (role !== 0 && role !== 1) throw new Error("Invalid PAKE role");
    if (!Object.hasOwn(groups, curve))
      throw new Error("Unsupported PAKE curve");
    this.group = groups[curve];
    if (purpose !== undefined) this.ids = identities(purpose, room ?? "");
    this.password = password.slice();
    const [ux, uy, vx, vy] = fixed[curve];
    this.u = this.point(ux, uy);
    this.v = this.point(vx, vy);
    if (role === 0) {
      this.secret = randomBytes(32);
      this.x = this.multiply(this.u, this.password).add(
        this.multiply(this.group.BASE, this.secret),
      );
    }
  }
  private point(x: bigint, y: bigint) {
    if (x < 0n || y < 0n) throw new Error("Invalid PAKE coordinate");
    let point: Point;
    if (this.curve === "ed25519") {
      if (y !== 0n) throw new Error("Invalid Edwards PAKE coordinate");
      point = this.group.fromBytes(integerBytes(x, 32));
    } else {
      if (x >= this.group.Fp.ORDER || y >= this.group.Fp.ORDER)
        throw new Error("PAKE coordinate out of range");
      point = this.group.fromAffine({ x, y });
    }
    point.assertValidity();
    if (point.is0() || point.isSmallOrder())
      throw new Error("Invalid PAKE point");
    return point;
  }
  private coords(point?: Point): [bigint | null, bigint | null] {
    if (!point) return [null, null];
    if (this.curve === "ed25519") return [integer(point.toBytes()), 0n];
    const { x, y } = point.toAffine();
    return [x, y];
  }
  private multiply(point: Point, bytes: Uint8Array) {
    let scalar: bigint;
    if (this.curve === "ed25519") {
      // Go left-pads short inputs, truncates long ones, then clamps as little endian.
      const key = new Uint8Array(32);
      if (bytes.length >= 32) key.set(bytes.subarray(0, 32));
      else key.set(bytes, 32 - bytes.length);
      key[0] &= 248;
      key[31] &= 127;
      key[31] |= 64;
      scalar = integer(key.reverse());
    } else scalar = integer(bytes);
    scalar %= this.group.Fn.ORDER;
    return scalar === 0n ? this.group.ZERO : point.multiply(scalar);
  }
  bytes() {
    const [ux, uy, vx, vy] = fixed[this.curve];
    const [xx, xy] = this.coords(this.x);
    const [yx, yy] = this.coords(this.y);
    // Object insertion order, decimal integers, Unicode field names and nulls match Go.
    return textEncoder.encode(
      stringify({
        Role: this.role,
        Uᵤ: ux,
        Uᵥ: uy,
        Vᵤ: vx,
        Vᵥ: vy,
        Xᵤ: xx,
        Xᵥ: xy,
        Yᵤ: yx,
        Yᵥ: yy,
        P: null,
        Pw: null,
        Vpwᵤ: null,
        Vpwᵥ: null,
        Upwᵤ: null,
        Upwᵥ: null,
        Aα: null,
        Aαᵤ: null,
        Aαᵥ: null,
        Zᵤ: null,
        Zᵥ: null,
        K: null,
      })!,
    );
  }
  async update(bytes: Uint8Array) {
    if (bytes.length > 4096) throw new Error("PAKE message too large");
    const q = parse(
      new TextDecoder("utf-8", { fatal: true }).decode(bytes),
      null,
      (value) => {
        if (!/^(0|[1-9][0-9]{0,156})$/.test(value))
          throw new Error("Invalid PAKE integer");
        return BigInt(value);
      },
    ) as Record<string, unknown> | null;
    if (!q || q.Role !== BigInt(1 - this.role))
      throw new Error("Invalid peer PAKE role");
    const prefix = this.role === 1 ? "X" : "Y";
    const x = q[prefix + "ᵤ"],
      y = q[prefix + "ᵥ"];
    if (typeof x !== "bigint" || typeof y !== "bigint")
      throw new Error("Missing PAKE point");
    const peer = this.point(x, y);
    let z: Point;
    if (this.role === 1) {
      this.x = peer;
      this.secret = randomBytes(32);
      this.y = this.multiply(this.v, this.password).add(
        this.multiply(this.group.BASE, this.secret),
      );
      z = peer.subtract(this.multiply(this.u, this.password));
    } else {
      this.y = peer;
      z = peer.subtract(this.multiply(this.v, this.password));
    }
    z = this.multiply(z, this.secret!);
    const fields = [
      this.password,
      ...[this.x!, this.y!, z].flatMap((p) =>
        this.coords(p).map((n) => integerBytes(n!)),
      ),
    ];
    const transcript = this.ids
      ? frame(
          textEncoder.encode(
            "github.com/schollz/pake/v3/identity-bound-session-key/v1",
          ),
          fields[0],
          ...this.ids,
          textEncoder.encode(this.curve),
          ...fields.slice(1),
        )
      : concatBytes(...fields);
    return { bytes: this.bytes(), key: await sha256(transcript) };
  }
  release() {
    this.password.fill(0);
    this.secret?.fill(0);
    this.x = undefined;
    this.y = undefined;
  }
}
