import * as bare from "libapps";
export declare class Hterm {
    elem: HTMLElement;
    term: bare.hterm.Terminal;
    io: bare.hterm.IO;
    columns: number;
    rows: number;
    message: string;
    constructor(elem: HTMLElement);
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
    deactivate(): void;
    reset(): void;
    close(): void;
}
