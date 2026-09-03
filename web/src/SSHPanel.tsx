import { useEffect, useRef, useState } from "react";
import { Eye, EyeOff, LogIn, ShieldAlert, SquareTerminal, X } from "lucide-react";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { sshEvents, trackSSHEvent } from "./analytics";
import { errorMessage, textEncoder } from "./protocol/bytes";
import {
  SSHJoinSession,
  type SSHRole,
  type SSHTerminalSize,
} from "./protocol/ssh";
import type { TransferSettings } from "./protocol/types";

type Props = {
  settings: TransferSettings;
  theme: "dark" | "light";
  onActiveChange(active: boolean): void;
};

function terminalTheme(theme: Props["theme"]) {
  return theme === "dark"
    ? {
        background: "#0d100d",
        foreground: "#e7e9df",
        cursor: "#bbd66b",
        cursorAccent: "#0d100d",
        selectionBackground: "#526431",
      }
    : {
        background: "#fbfaf5",
        foreground: "#252a21",
        cursor: "#526b24",
        cursorAccent: "#fbfaf5",
        selectionBackground: "#cddca5",
      };
}

function roleLabel(role: SSHRole) {
  return role === "read-write" ? "Read/write" : "Read-only";
}

export default function SSHPanel({ settings, theme, onActiveChange }: Props) {
  const [code, setCode] = useState("");
  const [showCode, setShowCode] = useState(false);
  const [busy, setBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [role, setRole] = useState<SSHRole>();
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const container = useRef<HTMLDivElement>(null);
  const terminal = useRef<Terminal>(null);
  const fit = useRef<FitAddon>(null);
  const session = useRef<SSHJoinSession>(null);
  const activeRole = useRef<SSHRole | undefined>(undefined);
  const intentionalDisconnect = useRef(false);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const instance = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      disableStdin: true,
      fontFamily: '"JetBrains Mono Variable", "SFMono-Regular", Consolas, monospace',
      fontSize: 14,
      lineHeight: 1.15,
      scrollback: 10_000,
      theme: terminalTheme(theme),
    });
    const fitAddon = new FitAddon();
    instance.loadAddon(fitAddon);
    instance.open(container.current!);
    fitAddon.fit();
    terminal.current = instance;
    fit.current = fitAddon;

    const input = instance.onData((data) => {
      if (activeRole.current === "read-write") {
        session.current?.sendInput(textEncoder.encode(data));
      }
    });
    instance.attachCustomKeyEventHandler((event) => {
      if (event.type === "keydown" && event.ctrlKey && event.key === "]") {
        intentionalDisconnect.current = true;
        session.current?.disconnect();
        return false;
      }
      return true;
    });

    let resizeFrame = 0;
    const resize = () => {
      window.cancelAnimationFrame(resizeFrame);
      resizeFrame = window.requestAnimationFrame(() => {
        fitAddon.fit();
        session.current?.resize({ width: instance.cols, height: instance.rows });
      });
    };
    const observer = new ResizeObserver(resize);
    observer.observe(container.current!);

    return () => {
      mounted.current = false;
      intentionalDisconnect.current = true;
      session.current?.disconnect();
      observer.disconnect();
      window.cancelAnimationFrame(resizeFrame);
      input.dispose();
      instance.dispose();
      terminal.current = null;
      fit.current = null;
    };
  }, []);

  useEffect(() => {
    if (terminal.current) terminal.current.options.theme = terminalTheme(theme);
  }, [theme]);

  useEffect(() => {
    activeRole.current = role;
    if (terminal.current) {
      terminal.current.options.disableStdin = !connected || role === "read-only";
      if (connected && role === "read-write") terminal.current.focus();
    }
  }, [connected, role]);

  async function writeOutput(bytes: Uint8Array) {
    const instance = terminal.current;
    if (!instance) throw new Error("Terminal is unavailable");
    await new Promise<void>((resolve) => instance.write(bytes, resolve));
  }

  function currentSize(): SSHTerminalSize {
    fit.current?.fit();
    return {
      width: terminal.current?.cols || 80,
      height: terminal.current?.rows || 24,
    };
  }

  async function join() {
    const invitation = code.trim();
    if (!invitation || busy) return;
    intentionalDisconnect.current = false;
    setBusy(true);
    setConnected(false);
    setRole(undefined);
    setError("");
    terminal.current?.reset();
    onActiveChange(true);

    let sessionTracked = false;

    const active = new SSHJoinSession({
      code: invitation,
      settings,
      size: currentSize(),
      callbacks: {
        onStatus: (message) => mounted.current && setStatus(message),
        onRole: (nextRole) => {
          if (!mounted.current) return;
          setRole(nextRole);
          if (nextRole) setCode("");
        },
        onConnected: (value) => {
          if (!mounted.current) return;
          if (value && !sessionTracked) {
            sessionTracked = true;
            trackSSHEvent(sshEvents.browserSession);
          }
          setConnected(value);
        },
        onOutput: writeOutput,
        onReconnect: () => terminal.current?.reset(),
      },
    });
    session.current = active;
    try {
      await active.done;
    } catch (reason) {
      if (mounted.current && !intentionalDisconnect.current) {
        setError(errorMessage(reason));
        setStatus("Could not join the shared terminal");
      } else if (mounted.current) {
        setStatus("Disconnected");
      }
    } finally {
      if (session.current === active) session.current = null;
      if (mounted.current) {
        setBusy(false);
        setConnected(false);
        onActiveChange(false);
      }
    }
  }

  function disconnect() {
    intentionalDisconnect.current = true;
    setStatus("Disconnecting…");
    session.current?.disconnect();
  }

  return (
    <section className="ssh-workspace" aria-label="SSH terminal">
      <article className="panel ssh-panel">
        <div className="panel-heading ssh-heading">
          <span className="step">
            <SquareTerminal aria-hidden="true" />
          </span>
          <div>
            <h2>Join a shared terminal</h2>
            <p>
              Paste the invitation printed by a running <code>croc ssh</code>{" "}
              host.
            </p>
          </div>
          {role && <span className={`ssh-role ${role}`}>{roleLabel(role)}</span>}
        </div>

        {!role && (
          <form
            className="ssh-join-form"
            autoComplete="off"
            onSubmit={(event) => {
              event.preventDefault();
              void join();
            }}
          >
            <div className="ssh-invitation-field">
              <label htmlFor="croc-ssh-invitation">SSH invitation</label>
              <span className="ssh-code-input">
                <input
                  id="croc-ssh-invitation"
                  type={showCode ? "text" : "password"}
                  name="croc-ssh-invitation"
                  autoComplete="off"
                  spellCheck={false}
                  readOnly
                  value={code}
                  disabled={busy}
                  placeholder="six-word-invitation"
                  onPaste={(event) => {
                    event.preventDefault();
                    setCode(event.clipboardData.getData("text"));
                  }}
                />
                <button
                  type="button"
                  aria-label={
                    showCode ? "Hide SSH invitation" : "Show SSH invitation"
                  }
                  title={showCode ? "Hide invitation" : "Show invitation"}
                  disabled={busy}
                  onClick={() => setShowCode((value) => !value)}
                >
                  {showCode ? <EyeOff /> : <Eye />}
                </button>
              </span>
            </div>
            <button
              className="primary-button"
              type="submit"
              disabled={busy || !code.trim()}
            >
              <LogIn /> Join terminal
            </button>
          </form>
        )}

        {status && (
          <div className="ssh-status-row" aria-live="polite">
            <span className={`status-dot${connected ? " connected" : ""}`} />
            <span>{status}</span>
          </div>
        )}
        {error && <p className="status-message error">{error}</p>}

        <div
          ref={container}
          className="ssh-terminal"
          aria-label="Shared terminal output"
        />

        <div className="ssh-actions">
          {busy && (
            <button
              className="primary-button inverted"
              type="button"
              onClick={disconnect}
            >
              <X /> {connected ? "Disconnect" : "Cancel"}
            </button>
          )}
          <p>
            <ShieldAlert aria-hidden="true" /> Read/write access uses the host
            account’s privileges. Read-only viewers can still see sensitive terminal
            output.
          </p>
        </div>
      </article>
    </section>
  );
}
