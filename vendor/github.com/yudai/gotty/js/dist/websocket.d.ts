export declare class ConnectionFactory {
    url: string;
    protocols: string[];
    constructor(url: string, protocols: string[]);
    create(): Connection;
}
export declare class Connection {
    bare: WebSocket;
    constructor(url: string, protocols: string[]);
    open(): void;
    close(): void;
    send(data: string): void;
    isOpen(): boolean;
    onOpen(callback: () => void): void;
    onReceive(callback: (data: string) => void): void;
    onClose(callback: () => void): void;
}
