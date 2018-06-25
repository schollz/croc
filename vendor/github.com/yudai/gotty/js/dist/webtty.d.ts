export declare const protocols: string[];
export declare const msgInputUnknown = "0";
export declare const msgInput = "1";
export declare const msgPing = "2";
export declare const msgResizeTerminal = "3";
export declare const msgUnknownOutput = "0";
export declare const msgOutput = "1";
export declare const msgPong = "2";
export declare const msgSetWindowTitle = "3";
export declare const msgSetPreferences = "4";
export declare const msgSetReconnect = "5";
export interface Terminal {
    info(): {
        columns: number;
        rows: number;
    };
    output(data: string): void;
    showMessage(message: string, timeout: number): void;
    removeMessage(): void;
    setWindowTitle(title: string): void;
    setPreferences(value: object): void;
    onInput(callback: (input: string) => void): void;
    onResize(callback: (colmuns: number, rows: number) => void): void;
    reset(): void;
    deactivate(): void;
    close(): void;
}
export interface Connection {
    open(): void;
    close(): void;
    send(data: string): void;
    isOpen(): boolean;
    onOpen(callback: () => void): void;
    onReceive(callback: (data: string) => void): void;
    onClose(callback: () => void): void;
}
export interface ConnectionFactory {
    create(): Connection;
}
export declare class WebTTY {
    term: Terminal;
    connectionFactory: ConnectionFactory;
    args: string;
    authToken: string;
    reconnect: number;
    constructor(term: Terminal, connectionFactory: ConnectionFactory, args: string, authToken: string);
    open(): () => void;
}
