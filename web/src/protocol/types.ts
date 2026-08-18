export type MessageType =
  | "pake"
  | "pake-confirm"
  | "externalip"
  | "finished"
  | "error"
  | "close-recipient"
  | "close-sender"
  | "recipientready"
  | "fileinfo";

export interface CrocMessage {
  t: MessageType;
  v?: number;
  m?: string;
  b?: Uint8Array;
  b2?: Uint8Array;
  n?: number;
}

export interface WireFileInfo {
  n?: string;
  fr?: string;
  fs?: string;
  h?: string;
  s?: number;
  m?: string;
  c?: boolean;
  e?: boolean;
  sy?: string;
  md?: number;
  tf?: boolean;
  ig?: boolean;
}

export interface SenderInfoWire {
  FilesToTransfer: WireFileInfo[] | null;
  EmptyFoldersToTransfer: WireFileInfo[] | null;
  TotalNumberFolders: number;
  MachineID: string;
  Ask: boolean;
  SendingText: boolean;
  NoCompress: boolean;
  HashAlgorithm: string;
  ReconnectVersion?: number;
  NextReconnectRoom?: string;
  Features?: string[];
}

export interface RemoteFileRequestWire {
  CurrentFileChunkRanges: number[] | null;
  FilesToTransferCurrentNum: number;
  MachineID: string;
  ReconnectVersion: number;
  Features?: string[];
}

export interface OfferedFile {
  name: string;
  folder: string;
  path: string;
  size: number;
  hash: Uint8Array;
  modified?: string;
  mode?: number;
  compressed?: boolean;
}

export interface TransferOffer {
  kind: "files" | "text";
  files: OfferedFile[];
  emptyFolders: string[];
  totalSize: number;
  senderMachineID: string;
  noCompress: boolean;
  perFileCompression: boolean;
}

export const maxTextTransferBytes = 1024 * 1024;

export interface PreparedFile {
  file: File;
  name: string;
  size: number;
  hash: Uint8Array;
  modified: string;
  compressed?: boolean;
}

export interface TransferSettings {
  gatewayURL: string;
  relayAddresses: string[];
  relayPassword: string;
  storeAPI: string;
}

export interface FileProgress {
  fileIndex: number;
  fileCount: number;
  fileName: string;
  fileBytes: number;
  fileSize: number;
  totalBytes: number;
  totalSize: number;
}

export interface TransferCallbacks {
  onStatus?(status: string): void;
  onProgress?(progress: FileProgress): void;
  onFileComplete?(fileName: string): void;
}

export interface ReceiveSink {
  writeAt(position: number, bytes: Uint8Array): Promise<void>;
  finalize(): Promise<void>;
  hash(algorithm?: "xxhash" | "sha256"): Promise<Uint8Array>;
  commit(): Promise<void>;
  abort(): Promise<void>;
}

export interface ReceiveDestination {
  createEmptyFolder(path: string): Promise<void>;
  openFile(file: OfferedFile): Promise<ReceiveSink>;
}

export interface ReceiveCallbacks extends TransferCallbacks {
  onOffer(offer: TransferOffer): Promise<ReceiveDestination | false>;
}
