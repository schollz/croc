import { useEffect, useId, useState } from "react";
import { QrCode } from "lucide-react";
import { QRCodeSVG } from "qrcode.react";

type ShareQRCodeProps = {
  value: string;
  description: string;
  disabled?: boolean;
};

export function makeDirectReceiveURL(
  code: string,
  origin = window.location.origin,
) {
  const receiveURL = new URL("/", origin);
  receiveURL.searchParams.set("code", code.trim());
  return receiveURL.href;
}

export function ShareQRCode({
  value,
  description,
  disabled = false,
}: ShareQRCodeProps) {
  const [open, setOpen] = useState(false);
  const regionID = useId();

  useEffect(() => {
    if (disabled) setOpen(false);
  }, [disabled]);

  return (
    <div className="share-qr">
      <button
        type="button"
        className="secondary-button share-qr-toggle"
        aria-expanded={open}
        aria-controls={regionID}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        <QrCode aria-hidden="true" />
        {open ? "Hide QR code" : "Show QR code"}
      </button>
      {open && (
        <div
          id={regionID}
          className="share-qr-region"
          role="region"
          aria-label="Receiver QR code"
        >
          <div className="share-qr-code">
            <QRCodeSVG
              value={value}
              size={224}
              level="M"
              marginSize={4}
              bgColor="#ffffff"
              fgColor="#050605"
              title="Scan to open the croc receive page"
            />
          </div>
          <p>{description}</p>
        </div>
      )}
    </div>
  );
}
