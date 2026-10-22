import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { TaskLauncher, TaskNavigation, taskFromHash, useTaskNavigation } from "./TaskLauncher";

beforeEach(() => window.history.replaceState({}, "", "/"));
afterEach(cleanup);

function Harness({ storeEnabled = true, restored = false } = {}) {
  const [locked, setLocked] = useState(false);
  const [task, navigate] = useTaskNavigation({
    storeEnabled,
    initialTask: restored ? "store" : "home",
    locked,
  });
  return (
    <>
      {task === "home" ? (
        <TaskLauncher storeEnabled={storeEnabled} onSelect={navigate} />
      ) : (
        <>
          <TaskNavigation task={task} disabled={locked} onBack={() => navigate("home")} />
          <output aria-label="Selected task">{task}</output>
          <button onClick={() => setLocked(true)}>Start activity</button>
        </>
      )}
    </>
  );
}

describe("task launcher", () => {
  it.each([
    ["Send live", "send", "#send-panel"],
    ["Store files", "store", "#store"],
    ["Receive files", "receive", "#receive"],
    ["Join SSH", "ssh", "#ssh"],
  ])("opens %s directly and returns to all tasks", (label, task, hash) => {
    render(<Harness />);
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: label }));
    expect(screen.getByLabelText("Selected task")).toHaveTextContent(task);
    expect(window.location.hash).toBe(hash);
    fireEvent.click(screen.getByRole("button", { name: "All tasks" }));
    expect(screen.getByText("What would you like to do?")).toBeVisible();
    expect(window.location.hash).toBe("");
  });

  it("offers only supported tasks when storage is disabled", () => {
    window.history.replaceState({}, "", "/#store");
    render(<Harness storeEnabled={false} />);
    expect(screen.queryByRole("button", { name: "Store files" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send live" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Receive files" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Join SSH" })).toBeVisible();
  });

  it("keeps an active task open when navigation is attempted", () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: "Send live" }));
    fireEvent.click(screen.getByRole("button", { name: "Start activity" }));
    expect(screen.getByRole("button", { name: "All tasks" })).toBeDisabled();
    act(() => {
      window.history.replaceState({}, "", "/#receive");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(screen.getByLabelText("Selected task")).toHaveTextContent("send");
    expect(window.location.hash).toBe("#send-panel");
  });

  it("follows browser navigation between tasks and the launcher", () => {
    render(<Harness />);
    act(() => {
      window.history.replaceState({}, "", "/#receive");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(screen.getByLabelText("Selected task")).toHaveTextContent("receive");
    act(() => {
      window.history.replaceState({}, "", "/");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(screen.getByText("What would you like to do?")).toBeVisible();
  });

  it("opens restored uploads in the stored task", () => {
    render(<Harness restored />);
    expect(screen.getByLabelText("Selected task")).toHaveTextContent("store");
  });

  it("keeps existing browser entry links working", () => {
    expect(taskFromHash("#send-panel")).toBe("send");
    expect(taskFromHash("#send")).toBe("send");
    expect(taskFromHash("#receive")).toBe("receive");
    expect(taskFromHash("#ssh?code=invitation")).toBe("ssh");
    expect(taskFromHash("#cli-download")).toBe("home");
  });
});
