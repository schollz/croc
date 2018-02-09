import * as bare from "libapps";

export class Hterm {
    elem: HTMLElement;

    term: bare.hterm.Terminal;
    io: bare.hterm.IO;

    columns: number;
    rows: number;

    // to "show" the current message when removeMessage() is called
    message: string;

    constructor(elem: HTMLElement) {
        this.elem = elem;
        bare.hterm.defaultStorage = new bare.lib.Storage.Memory();
        this.term = new bare.hterm.Terminal();
        this.term.getPrefs().set("send-encoding", "raw");
        this.term.decorate(this.elem);

        this.io = this.term.io.push();
        this.term.installKeyboard();
    };

    info(): { columns: number, rows: number } {
        return { columns: this.columns, rows: this.rows };
    };

    output(data: string) {
        if (this.term.io != null) {
            this.term.io.writeUTF8(data);
        }
    };

    showMessage(message: string, timeout: number) {
        this.message = message;
        if (timeout > 0) {
            this.term.io.showOverlay(message, timeout);
        } else {
            this.term.io.showOverlay(message, null);
        }
    };

    removeMessage(): void {
        // there is no hideOverlay(), so show the same message with 0 sec
        this.term.io.showOverlay(this.message, 0);
    }

    setWindowTitle(title: string) {
        this.term.setWindowTitle(title);
    };

    setPreferences(value: object) {
        Object.keys(value).forEach((key) => {
            this.term.getPrefs().set(key, value[key]);
        });
    };

    onInput(callback: (input: string) => void) {
        this.io.onVTKeystroke = (data) => {
            callback(data);
        };
        this.io.sendString = (data) => {
            callback(data);
        };
    };

    onResize(callback: (colmuns: number, rows: number) => void) {
        this.io.onTerminalResize = (columns: number, rows: number) => {
            this.columns = columns;
            this.rows = rows;
            callback(columns, rows);
        };
    };

    deactivate(): void {
        this.io.onVTKeystroke    = function(){};
        this.io.sendString       = function(){};
        this.io.onTerminalResize = function(){};
        this.term.uninstallKeyboard();
    }

    reset(): void {
        this.removeMessage();
        this.term.installKeyboard();
    }

    close(): void {
        this.term.uninstallKeyboard();
    }
}
