import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, LogIn, RefreshCw, Unplug } from "lucide-react";
import { TunnelSession } from "./protocol/tunnel";
import { TunnelPreview, type PreviewNavigation } from "./tunnel-preview";
import type { TransferSettings } from "./protocol/types";
import { errorMessage } from "./protocol/bytes";

export default function TunnelPanel({
  initialCode = "",
  settings,
  onActiveChange,
}: {
  initialCode?: string;
  settings: TransferSettings;
  onActiveChange(active: boolean): void;
}) {
  const [code, setCode] = useState(initialCode);
  const [busy, setBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [status, setStatus] = useState(
    "Share a local web server with croc tunnel 5173, then enter its invitation.",
  );
  const [error, setError] = useState("");
  const [navigation, setNavigation] = useState<PreviewNavigation>({ url: "/" });
  const [revision, setRevision] = useState(0);
  const [history, setHistory] = useState(["/"]);
  const [position, setPosition] = useState(0);
  const [targetPort, setTargetPort] = useState(0);
  const session = useRef<TunnelSession | undefined>(undefined);
  const frame = useRef<HTMLIFrameElement>(null);
  const preview = useRef<TunnelPreview | undefined>(undefined);
  const mounted = useRef(true);
  const autoJoined = useRef(false);
  const reloadTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );

  function navigate(next: PreviewNavigation) {
    const local = new URL(next.url, `http://localhost:${targetPort}`);
    const path = local.pathname + local.search + local.hash;
    setHistory((previous) => [...previous.slice(0, position + 1), path]);
    setPosition(position + 1);
    setNavigation({ ...next, url: path });
    setError("");
  }
  function disconnect() {
    session.current?.disconnect();
    session.current = undefined;
    preview.current?.close();
    preview.current = undefined;
    setConnected(false);
    setBusy(false);
    setCode("");
    onActiveChange(false);
  }
  function join(secret = code) {
    if (session.current) return;
    setBusy(true);
    setError("");
    onActiveChange(true);
    const active = new TunnelSession(secret.trim(), settings, {
      state(isConnected, message, port) {
        if (!mounted.current) return;
        setConnected(isConnected);
        setStatus(message);
        if (port) {
          setTargetPort(port);
          setRevision((value) => value + 1);
          setCode("");
        }
      },
      reload() {
        clearTimeout(reloadTimer.current);
        reloadTimer.current = setTimeout(() => {
          if (mounted.current) setRevision((value) => value + 1);
        }, 100);
      },
    });
    session.current = active;
    void active.done
      .catch((reason) => {
        if (mounted.current) setError(errorMessage(reason));
      })
      .finally(() => {
        if (session.current !== active) return;
        session.current = undefined;
        if (mounted.current) {
          setBusy(false);
          setConnected(false);
          onActiveChange(false);
        }
      });
  }
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      clearTimeout(reloadTimer.current);
      session.current?.disconnect();
      preview.current?.close();
      onActiveChange(false);
    };
  }, [onActiveChange]);
  useEffect(() => {
    const timer = setTimeout(() => {
      if (initialCode && !autoJoined.current) {
        autoJoined.current = true;
        join(initialCode);
      }
    }, 0);
    return () => clearTimeout(timer);
    // Invitations are consumed once, like SSH deep links.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialCode]);
  useEffect(() => {
    if (!connected || !frame.current || !session.current) return;
    const next = new TunnelPreview(frame.current, session.current, targetPort, {
      navigate,
      error(message) {
        if (mounted.current) setError(message);
      },
    });
    preview.current = next;
    let cancelled = false;
    void next.load(navigation).catch((reason) => {
      if (!cancelled) setError(errorMessage(reason));
    });
    return () => {
      cancelled = true;
      next.close();
      if (preview.current === next) preview.current = undefined;
    };
    // Navigation closures are refreshed whenever the displayed page changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, targetPort, navigation, revision]);

  return (
    <section className="tunnel-workspace" aria-label="Tunnel workspace">
      <article className="panel">
        <h2>Open a shared web app</h2>
        <p className="status" role="status">
          {status}
        </p>
        {!busy && (
          <form
            onSubmit={(event) => {
              event.preventDefault();
              join();
            }}
          >
            <label htmlFor="tunnel-code">Tunnel invitation</label>
            <div className="tunnel-controls">
              <input
                id="tunnel-code"
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={code}
                onChange={(event) => setCode(event.target.value)}
                placeholder="six-word-invitation"
              />
              <button type="submit" disabled={!code.trim()}>
                <LogIn size={16} /> Connect
              </button>
            </div>
          </form>
        )}
        {busy && (
          <div className="tunnel-controls">
            <button
              type="button"
              disabled={!connected || position === 0}
              aria-label="Previous preview page"
              onClick={() => {
                setPosition(position - 1);
                setNavigation({ url: history[position - 1] });
              }}
            >
              <ArrowLeft size={16} />
            </button>
            <button
              type="button"
              disabled={!connected || position >= history.length - 1}
              aria-label="Next preview page"
              onClick={() => {
                setPosition(position + 1);
                setNavigation({ url: history[position + 1] });
              }}
            >
              <ArrowRight size={16} />
            </button>
            <code className="tunnel-path">{navigation.url}</code>
            <button
              type="button"
              disabled={!connected}
              aria-label="Reload preview"
              onClick={() => {
                setError("");
                setRevision((value) => value + 1);
              }}
            >
              <RefreshCw size={16} />
            </button>
            <button type="button" onClick={disconnect}>
              <Unplug size={16} /> Disconnect
            </button>
          </div>
        )}
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <p className="tunnel-note">
          Preview supports apps and APIs on one port. Changes refresh the page
          automatically. Apps requiring cookie login, browser storage, workers,
          or other origins can use CLI forwarding.
        </p>
      </article>
      {connected && (
        <iframe
          ref={frame}
          className="tunnel-frame"
          title="Shared web app"
          sandbox="allow-scripts allow-forms"
          referrerPolicy="no-referrer"
        />
      )}
    </section>
  );
}
