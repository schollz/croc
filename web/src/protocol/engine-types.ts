export interface PakeStart {
  handle: number;
  bytes: Uint8Array;
}

export interface PakeFinish {
  bytes: Uint8Array;
  key: Uint8Array;
}

export interface PeerKeys {
  key: Uint8Array;
  confirmationA: Uint8Array;
  confirmationB: Uint8Array;
}

export interface CodeComponents {
  room: string;
  passphrase: string;
  format: "legacy" | "three-word" | "four-word";
}

export interface SSHCodeComponents {
  room: string;
  passphrase: string;
}
