export class ConnectionFactory {
    url: string;
    protocols: string[];

    constructor(url: string, protocols: string[]) {
        this.url = url;
        this.protocols = protocols;
    };

    create(): Connection {
        return new Connection(this.url, this.protocols);
    };
}

export class Connection {
    bare: WebSocket;


    constructor(url: string, protocols: string[]) {
        this.bare = new WebSocket(url, protocols);
    }

    open() {
        // nothing todo for websocket
    };

    close() {
        this.bare.close();
    };

    send(data: string) {
        this.bare.send(data);
    };

    isOpen(): boolean {
        if (this.bare.readyState == WebSocket.CONNECTING ||
            this.bare.readyState == WebSocket.OPEN) {
            return true
        }
        return false
    }

    onOpen(callback: () => void) {
        this.bare.onopen = (event) => {
            callback();
        }
    };

    onReceive(callback: (data: string) => void) {
        this.bare.onmessage = (event) => {
            callback(event.data);
        }
    };

    onClose(callback: () => void) {
        this.bare.onclose = (event) => {
            callback();
        };
    };
}
