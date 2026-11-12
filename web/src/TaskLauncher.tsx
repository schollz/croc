import { useEffect, useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Download, Link, SquareTerminal, Upload } from "lucide-react";

export type Task = "home" | "send" | "store" | "receive" | "ssh";

const taskLabels: Record<Exclude<Task, "home">, string> = {
  send: "Send files",
  store: "Store files",
  receive: "Receive files",
  ssh: "Join SSH",
};

export function taskFromHash(hash = window.location.hash): Task {
  switch (hash.split("?", 1)[0]) {
    case "#send":
    case "#send-panel":
      return "send";
    case "#store":
      return "store";
    case "#receive":
      return "receive";
    case "#ssh":
      return "ssh";
    default:
      return "home";
  }
}

export function sshInvitationFromHash(hash = window.location.hash) {
  if (taskFromHash(hash) !== "ssh") return "";
  const queryAt = hash.indexOf("?");
  return queryAt < 0 ? "" : new URLSearchParams(hash.slice(queryAt + 1)).get("code")?.trim() ?? "";
}

function taskURL(task: Task) {
  const url = new URL(window.location.href);
  url.hash = task === "home" ? "" : task === "send" ? "send-panel" : task;
  return url;
}

export function useTaskNavigation({
  defaultTask = "home",
  initialTask = defaultTask,
  storeEnabled = true,
  locked = false,
}: {
  defaultTask?: "home" | "send";
  initialTask?: Task;
  storeEnabled?: boolean;
  locked?: boolean;
} = {}) {
  const availableTask = (task: Task) =>
    task === "home" || (task === "store" && !storeEnabled) ? defaultTask : task;
  const [task, setTask] = useState<Task>(() =>
    availableTask(window.location.hash ? taskFromHash() : initialTask),
  );
  const [invitation, setInvitation] = useState(() => sshInvitationFromHash());
  const current = useRef({ task, locked });
  current.current = { task, locked };

  useEffect(() => {
    function scrubInvitation() {
      if (sshInvitationFromHash()) {
        window.history.replaceState(window.history.state, "", taskURL("ssh"));
      }
    }
    function syncTask() {
      if (current.current.locked) {
        window.history.replaceState(window.history.state, "", taskURL(current.current.task));
        return;
      }
      setTask(availableTask(taskFromHash()));
      setInvitation(sshInvitationFromHash());
      scrubInvitation();
    }
    scrubInvitation();
    window.addEventListener("hashchange", syncTask);
    return () => {
      window.removeEventListener("hashchange", syncTask);
    };
  }, [defaultTask, storeEnabled]);

  function navigate(next: Task) {
    if (current.current.locked) return;
    next = availableTask(next);
    setTask(next);
    setInvitation("");
    const url = taskURL(next);
    if (url.href !== window.location.href) {
      window.history.pushState(window.history.state, "", url);
    }
  }

  return [task, navigate, invitation] as const;
}

export function TaskSwitcher({
  task,
  storeEnabled,
  disabled,
  onSelect,
}: {
  task: Task;
  storeEnabled: boolean;
  disabled: boolean;
  onSelect(task: Exclude<Task, "home">): void;
}) {
  const tasks = [
    { task: "send", icon: Upload },
    ...(storeEnabled ? [{ task: "store" as const, icon: Link }] : []),
    { task: "receive", icon: Download },
    { task: "ssh", icon: SquareTerminal },
  ] as const;

  return (
    <nav className="task-switcher" aria-label="Choose a task">
      {tasks.map(({ task: nextTask, icon: Icon }) => (
        <button
          key={nextTask}
          type="button"
          aria-pressed={task === nextTask}
          disabled={disabled}
          onClick={() => onSelect(nextTask)}
        >
          <Icon aria-hidden="true" />
          {taskLabels[nextTask]}
        </button>
      ))}
    </nav>
  );
}

export function TaskLauncher({
  storeEnabled,
  onSelect,
}: {
  storeEnabled: boolean;
  onSelect(task: Exclude<Task, "home">): void;
}) {
  const tasks = [
    { task: "send", icon: Upload, description: "Send to someone who is online now." },
    ...(storeEnabled ? [{ task: "store" as const, icon: Link, description: "Share an encrypted link. Download later." }] : []),
    { task: "receive", icon: Download, description: "Open a croc code or encrypted link." },
    { task: "ssh", icon: SquareTerminal, description: "Join a shared terminal with an invitation." },
  ] as const;
  return (
    <section className="task-launcher" aria-labelledby="task-launcher-title">
      <div className="task-launcher-heading">
        <h2 id="task-launcher-title">What would you like to do?</h2>
      </div>
      <div className="task-launcher-grid">
        {tasks.map(({ task, icon: Icon, description }) => (
          <button
            key={task}
            type="button"
            className="task-card"
            aria-label={taskLabels[task]}
            onClick={() => onSelect(task)}
          >
            <Icon className="task-card-icon" aria-hidden="true" />
            <span><strong>{taskLabels[task]}</strong><small>{description}</small></span>
            <ArrowRight className="task-card-arrow" aria-hidden="true" />
          </button>
        ))}
      </div>
    </section>
  );
}

export function TaskNavigation({ task, disabled, onBack }: {
  task: Exclude<Task, "home">;
  disabled: boolean;
  onBack(): void;
}) {
  return (
    <nav className="task-navigation" aria-label={`${taskLabels[task]} navigation`}>
      <button type="button" disabled={disabled} onClick={onBack}>
        <ArrowLeft aria-hidden="true" /> All tasks
      </button>
    </nav>
  );
}
