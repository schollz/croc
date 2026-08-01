import { ShieldCheck, X } from "lucide-react";
import type { AnalyticsConsent } from "./analytics";

type PrivacyModalProps = {
  currentChoice: AnalyticsConsent | null;
  onChoose(choice: AnalyticsConsent): void;
  onClose(): void;
};

export function PrivacyModal({
  currentChoice,
  onChoose,
  onClose,
}: PrivacyModalProps) {
  const canClose = currentChoice !== null;

  return (
    <div className="privacy-backdrop">
      <div
        className="privacy-modal"
        role="dialog"
        aria-labelledby="privacy-title"
        aria-describedby="privacy-description"
      >
        {canClose && (
          <button
            className="privacy-close"
            type="button"
            aria-label="Close privacy choices"
            onClick={onClose}
          >
            <X aria-hidden="true" />
          </button>
        )}

        <div className="privacy-heading">
          <span className="privacy-icon">
            <ShieldCheck aria-hidden="true" />
          </span>
          <div>
            <p className="eyebrow">
              Privacy <span aria-hidden="true">/</span>{" "}
              {currentChoice === "accepted" ? "analytics on" : "analytics off"}
            </p>
            <h2 id="privacy-title">Optional analytics</h2>
          </div>
        </div>

        <p id="privacy-description" className="privacy-intro">
          croc uses essential browser storage for preferences and this choice.
          If allowed, Umami records successful transfer type and basic browser
          context. Transfers work the same if you reject.
        </p>

        <details className="privacy-details">
          <summary>What can be collected?</summary>
          <p>
            Event name, page path, browser context, referrer, and network data
            such as IP address. File contents, names, croc codes, link keys,
            query strings, and relay settings are excluded.
          </p>
        </details>

        <div className="privacy-actions">
          <button
            className="secondary-button"
            type="button"
            onClick={() => onChoose("rejected")}
          >
            Reject analytics
          </button>
          <button
            className="primary-button"
            type="button"
            onClick={() => onChoose("accepted")}
          >
            Allow analytics
          </button>
        </div>
        <p className="privacy-footnote">
          Change this anytime from “privacy choices” in the footer.
        </p>
      </div>
    </div>
  );
}
