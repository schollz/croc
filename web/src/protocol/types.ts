export type MessageType =
  | "pake"
  | "externalip"
  | "finished"
  | "error"
  | "close-recipient"
  | "close-sender"
  | "recipientready"
  | "fileinfo";

export interface CrocMessage {
  t: MessageType;
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
}

export interface RemoteFileRequestWire {
  CurrentFileChunkRanges: number[] | null;
  FilesToTransferCurrentNum: number;
  MachineID: string;
  ReconnectVersion: number;
}

export interface OfferedFile {
  name: string;
  folder: string;
  path: string;
  size: number;
  hash: Uint8Array;
  modified?: string;
  mode?: number;
}

export interface TransferOffer {
  files: OfferedFile[];
  emptyFolders: string[];
  totalSize: number;
  senderMachineID: string;
  noCompress: boolean;
}

export interface PreparedFile {
  file: File;
  name: string;
  size: number;
  hash: Uint8Array;
  modified: string;
}

export interface TransferSettings {
  gatewayURL: string;
  relayAddress: string;
  relayPassword: string;
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
  hash(): Promise<Uint8Array>;
  abort(): Promise<void>;
}

export interface ReceiveDestination {
  createEmptyFolder(path: string): Promise<void>;
  createEmptyFile(file: OfferedFile): Promise<void>;
  openFile(file: OfferedFile): Promise<ReceiveSink>;
}

export interface ReceiveCallbacks extends TransferCallbacks {
  onOffer(offer: TransferOffer): Promise<ReceiveDestination | false>;
}
