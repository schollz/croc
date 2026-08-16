import {
  BookOpenText,
  Download,
  Terminal,
  Upload,
} from "lucide-react";
import { FaGithub } from "react-icons/fa";

const crocWebsite = "https://infinitedigits.co/croc/";
const crocRepository = "https://github.com/schollz/croc";

export function TransferLinks() {
  return (
    <nav className="transfer-links" aria-label="More ways to transfer with croc">
      <a href="/#send-panel">
        <Upload aria-hidden="true" />
        <span><strong>Send in your browser</strong><small>No install needed</small></span>
      </a>
      <a href="/#receive">
        <Download aria-hidden="true" />
        <span><strong>Receive in your browser</strong><small>Paste a code or link</small></span>
      </a>
      <a href="/#cli-download">
        <Terminal aria-hidden="true" />
        <span><strong>Download the croc CLI</strong><small>Windows, macOS, and Linux</small></span>
      </a>
      <a href={crocWebsite} target="_blank" rel="noopener noreferrer">
        <BookOpenText aria-hidden="true" />
        <span><strong>Read the croc guide</strong><small>Install and usage docs</small></span>
      </a>
      <a href={crocRepository} target="_blank" rel="noopener noreferrer">
        <FaGithub aria-hidden="true" />
        <span><strong>Explore the codebase</strong><small>Source, issues, and releases</small></span>
      </a>
    </nav>
  );
}
