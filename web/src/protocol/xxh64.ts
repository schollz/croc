// XXH64, seed zero. All 64-bit arithmetic uses paired unsigned 32-bit words.
// The wire digest is eight bytes in big-endian order (cespare/xxhash Sum).
type U64 = [number, number]; // low, high
const P1: U64 = [0x85ebca87, 0x9e3779b1],
  P2: U64 = [0x27d4eb4f, 0xc2b2ae3d];
const P3: U64 = [0x9e3779f9, 0x165667b1],
  P4: U64 = [0xc2b2ae63, 0x85ebca77],
  P5: U64 = [0x165667c5, 0x27d4eb2f];
function add(a: U64, b: U64): U64 {
  const low = (a[0] >>> 0) + (b[0] >>> 0);
  return [
    low >>> 0,
    ((a[1] >>> 0) + (b[1] >>> 0) + (low >= 0x100000000 ? 1 : 0)) >>> 0,
  ];
}
function xor(a: U64, b: U64): U64 {
  return [(a[0] ^ b[0]) >>> 0, (a[1] ^ b[1]) >>> 0];
}
function mul(a: U64, b: U64): U64 {
  const a0 = a[0] & 65535,
    a1 = a[0] >>> 16,
    b0 = b[0] & 65535,
    b1 = b[0] >>> 16;
  const c0 = a0 * b0,
    c1 = (c0 >>> 16) + a1 * b0 + a0 * b1;
  const low = ((c0 & 65535) | ((c1 & 65535) << 16)) >>> 0;
  const high =
    (Math.imul(a[1], b[0]) +
      Math.imul(a[0], b[1]) +
      a1 * b1 +
      Math.floor(c1 / 65536)) >>>
    0;
  return [low, high];
}
function rot(a: U64, n: number): U64 {
  if (n === 32) return [a[1], a[0]];
  if (n > 32) return rot([a[1], a[0]], n - 32);
  return [
    ((a[0] << n) | (a[1] >>> (32 - n))) >>> 0,
    ((a[1] << n) | (a[0] >>> (32 - n))) >>> 0,
  ];
}
function shr(a: U64, n: number): U64 {
  return n >= 32
    ? [a[1] >>> (n - 32), 0]
    : [((a[0] >>> n) | (a[1] << (32 - n))) >>> 0, a[1] >>> n];
}
function read(v: DataView, at: number): U64 {
  return [v.getUint32(at, true), v.getUint32(at + 4, true)];
}
function round(a: U64, b: U64) {
  return mul(rot(add(a, mul(b, P2)), 31), P1);
}
function merge(a: U64, b: U64) {
  return add(mul(xor(a, round([0, 0], b)), P1), P4);
}
export class XXH64 {
  private length = 0;
  private tail = new Uint8Array(32);
  private tailLength = 0;
  private lanes: U64[] = [
    add(P1, P2),
    [...P2],
    [0, 0],
    add(xor(P1, [0xffffffff, 0xffffffff]), [1, 0]),
  ];
  private block(view: DataView, at = 0) {
    for (let i = 0; i < 4; i++)
      this.lanes[i] = round(this.lanes[i], read(view, at + i * 8));
  }
  update(input: Uint8Array) {
    this.length += input.length;
    let at = 0;
    if (this.tailLength + input.length < 32) {
      this.tail.set(input, this.tailLength);
      this.tailLength += input.length;
      return;
    }
    if (this.tailLength) {
      const take = 32 - this.tailLength;
      this.tail.set(input.subarray(0, take), this.tailLength);
      this.block(new DataView(this.tail.buffer));
      at = take;
      this.tailLength = 0;
    }
    const view = new DataView(input.buffer, input.byteOffset, input.byteLength);
    for (; at + 32 <= input.length; at += 32) this.block(view, at);
    this.tail.set(input.subarray(at));
    this.tailLength = input.length - at;
  }
  digest() {
    let hash: U64;
    if (this.length >= 32) {
      hash = add(
        add(rot(this.lanes[0], 1), rot(this.lanes[1], 7)),
        add(rot(this.lanes[2], 12), rot(this.lanes[3], 18)),
      );
      for (const lane of this.lanes) hash = merge(hash, lane);
    } else hash = [...P5];
    hash = add(hash, [
      this.length >>> 0,
      Math.floor(this.length / 0x100000000),
    ]);
    const view = new DataView(this.tail.buffer);
    let at = 0;
    for (; at + 8 <= this.tailLength; at += 8)
      hash = add(
        mul(rot(xor(hash, round([0, 0], read(view, at))), 27), P1),
        P4,
      );
    if (at + 4 <= this.tailLength) {
      hash = add(
        mul(rot(xor(hash, mul([view.getUint32(at, true), 0], P1)), 23), P2),
        P3,
      );
      at += 4;
    }
    for (; at < this.tailLength; at++)
      hash = mul(rot(xor(hash, mul([this.tail[at], 0], P5)), 11), P1);
    hash = xor(hash, shr(hash, 33));
    hash = mul(hash, P2);
    hash = xor(hash, shr(hash, 29));
    hash = mul(hash, P3);
    hash = xor(hash, shr(hash, 32));
    const result = new Uint8Array(8);
    const out = new DataView(result.buffer);
    out.setUint32(0, hash[1]);
    out.setUint32(4, hash[0]);
    return result;
  }
}
