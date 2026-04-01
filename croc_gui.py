"""
Croc GUI - Floating drag-and-drop interface for croc file transfer
Usage: python croc_gui.py [path/to/croc.exe]
"""

import tkinter as tk
from tkinter import ttk, messagebox, scrolledtext
import subprocess
import threading
import sys
import os
import re
import shutil
import time

try:
    from tkinterdnd2 import DND_FILES, TkinterDnD
    DND_AVAILABLE = True
except ImportError:
    DND_AVAILABLE = False

# ─── Color palette ────────────────────────────────────────────────────────────
BG_MAIN      = "#0f1117"
BG_CARD      = "#1a1d27"
BG_SURFACE   = "#22263a"
BG_HOVER     = "#2a2f47"
ACCENT       = "#6c63ff"
ACCENT_LIGHT = "#8b85ff"
ACCENT_DIM   = "#3d3970"
GREEN        = "#22d3a5"
GREEN_DIM    = "#134d3c"
RED          = "#ff5c7a"
RED_DIM      = "#4d1c28"
AMBER        = "#ffb347"
TEXT         = "#e8eaf6"
TEXT_MUTED   = "#7b82a8"
TEXT_DIM     = "#4a5080"
BORDER       = "#2d3155"

FONT_BODY    = ("Segoe UI", 10)
FONT_SMALL   = ("Segoe UI", 9)
FONT_TITLE   = ("Segoe UI", 11, "bold")
FONT_MONO    = ("Consolas", 10)

# ─── Croc finder ──────────────────────────────────────────────────────────────
def find_croc():
    """Find croc executable."""
    if len(sys.argv) > 1 and os.path.isfile(sys.argv[1]):
        return sys.argv[1]
    # Check same dir as script
    script_dir = os.path.dirname(os.path.abspath(__file__))
    for name in ["croc.exe", "croc"]:
        p = os.path.join(script_dir, name)
        if os.path.isfile(p):
            return p
    return shutil.which("croc")


# ─── Rounded canvas helpers ───────────────────────────────────────────────────
def rounded_rect(canvas, x1, y1, x2, y2, radius=12, **kwargs):
    pts = [
        x1 + radius, y1,
        x2 - radius, y1,
        x2, y1,
        x2, y1 + radius,
        x2, y2 - radius,
        x2, y2,
        x2 - radius, y2,
        x1 + radius, y2,
        x1, y2,
        x1, y2 - radius,
        x1, y1 + radius,
        x1, y1,
    ]
    return canvas.create_polygon(pts, smooth=True, **kwargs)


# ─── Main App class ───────────────────────────────────────────────────────────
class CrocGUI:
    def __init__(self):
        self.croc_path = find_croc()
        self.send_process = None
        self.queued_files = []
        self._drag_active = False
        self._offset_x = 0
        self._offset_y = 0

        # Root window
        if DND_AVAILABLE:
            self.root = TkinterDnD.Tk()
        else:
            self.root = tk.Tk()

        self.root.title("Croc Send")
        self.root.geometry("400x560")
        self.root.minsize(360, 480)
        self.root.configure(bg=BG_MAIN)
        self.root.wm_attributes("-topmost", True)
        self.root.wm_attributes("-alpha", 0.97)
        # Remove default title bar for custom one
        self.root.overrideredirect(True)

        # Shadow / outer frame
        self.root.configure(bg=BG_MAIN)
        self._build_ui()
        self._center_window()
        self._animate_in()

    def _center_window(self):
        self.root.update_idletasks()
        w = self.root.winfo_width()
        h = self.root.winfo_height()
        sw = self.root.winfo_screenwidth()
        sh = self.root.winfo_screenheight()
        x = sw - w - 40
        y = (sh - h) // 2
        self.root.geometry(f"+{x}+{y}")

    def _animate_in(self):
        self.root.wm_attributes("-alpha", 0)
        self.root.update()
        for alpha in range(0, 98, 8):
            self.root.wm_attributes("-alpha", alpha / 100)
            self.root.update()
            time.sleep(0.012)
        self.root.wm_attributes("-alpha", 0.97)

    # ── UI Build ─────────────────────────────────────────────────────────────
    def _build_ui(self):
        main = tk.Frame(self.root, bg=BG_CARD, bd=0)
        main.pack(fill="both", expand=True, padx=2, pady=2)

        # ── Title Bar ────────────────────────────────────────────────────────
        title_bar = tk.Frame(main, bg=BG_MAIN, height=42)
        title_bar.pack(fill="x")
        title_bar.pack_propagate(False)

        logo_lbl = tk.Label(title_bar, text="🐊  CROC", font=("Segoe UI", 12, "bold"),
                            fg=ACCENT_LIGHT, bg=BG_MAIN)
        logo_lbl.pack(side="left", padx=14)

        # Window control buttons
        btn_frame = tk.Frame(title_bar, bg=BG_MAIN)
        btn_frame.pack(side="right", padx=8)

        self._make_wm_btn(btn_frame, "×", RED, self._quit)
        self._make_wm_btn(btn_frame, "─", AMBER, self._minimize)

        # Always on top toggle
        self.topmost_var = tk.BooleanVar(value=True)
        pin_btn = tk.Label(title_bar, text="📌", font=("Segoe UI", 12),
                           fg=ACCENT, bg=BG_MAIN, cursor="hand2")
        pin_btn.pack(side="right", padx=4)
        pin_btn.bind("<Button-1>", self._toggle_topmost)
        self.pin_btn = pin_btn

        # Drag window via title bar
        for w in (title_bar, logo_lbl):
            w.bind("<ButtonPress-1>", self._start_drag)
            w.bind("<B1-Motion>", self._do_drag)

        # ── Separator ────────────────────────────────────────────────────────
        tk.Frame(main, bg=BORDER, height=1).pack(fill="x")

        # ── Croc path indicator ───────────────────────────────────────────────
        info_bar = tk.Frame(main, bg=BG_SURFACE, pady=4)
        info_bar.pack(fill="x")

        croc_status = "✓ " + (self.croc_path or "croc not found!")
        croc_color = GREEN if self.croc_path else RED
        croc_icon = tk.Label(info_bar, text=croc_status, font=FONT_SMALL,
                             fg=croc_color, bg=BG_SURFACE, padx=10)
        croc_icon.pack(side="left")

        if not self.croc_path:
            tk.Label(info_bar, text="Install croc and add to PATH",
                     font=FONT_SMALL, fg=TEXT_MUTED, bg=BG_SURFACE, padx=4).pack(side="left")

        # ── Tabs ──────────────────────────────────────────────────────────────
        tab_frame = tk.Frame(main, bg=BG_CARD)
        tab_frame.pack(fill="x", padx=12, pady=(10, 0))

        self.active_tab = tk.StringVar(value="send")
        self.tab_btns = {}
        for tab, label in [("send", "  📤 Gửi File  "), ("receive", "  📥 Nhận File  ")]:
            b = tk.Label(tab_frame, text=label, font=FONT_BODY, cursor="hand2",
                         padx=8, pady=6, bg=BG_CARD, fg=TEXT_MUTED)
            b.pack(side="left")
            b.bind("<Button-1>", lambda e, t=tab: self._switch_tab(t))
            self.tab_btns[tab] = b

        tk.Frame(main, bg=BORDER, height=1).pack(fill="x", padx=0, pady=(4, 0))

        # ── Content area ──────────────────────────────────────────────────────
        self.content = tk.Frame(main, bg=BG_CARD)
        self.content.pack(fill="both", expand=True, padx=12, pady=8)

        self._build_send_panel()
        self._build_receive_panel()
        self._switch_tab("send")

        # ── Log area ──────────────────────────────────────────────────────────
        tk.Frame(main, bg=BORDER, height=1).pack(fill="x")
        log_header = tk.Frame(main, bg=BG_MAIN, pady=4)
        log_header.pack(fill="x")
        tk.Label(log_header, text="📋 Log", font=FONT_SMALL, fg=TEXT_MUTED,
                 bg=BG_MAIN, padx=10).pack(side="left")
        clear_btn = tk.Label(log_header, text="Xóa", font=FONT_SMALL, fg=ACCENT,
                             bg=BG_MAIN, cursor="hand2", padx=10)
        clear_btn.pack(side="right")
        clear_btn.bind("<Button-1>", lambda e: self._clear_log())

        self.log_text = tk.Text(main, height=7, bg=BG_MAIN, fg=TEXT_MUTED,
                                font=("Consolas", 9), bd=0, padx=8, pady=4,
                                insertbackground=ACCENT, selectbackground=ACCENT_DIM,
                                wrap="word", state="disabled", cursor="arrow")
        self.log_text.pack(fill="both", expand=False, padx=0, pady=(0, 2))

        # Color tags for log
        self.log_text.tag_config("info",    foreground=TEXT_MUTED)
        self.log_text.tag_config("code",    foreground=GREEN)
        self.log_text.tag_config("error",   foreground=RED)
        self.log_text.tag_config("success", foreground=GREEN)
        self.log_text.tag_config("warn",    foreground=AMBER)

    def _make_wm_btn(self, parent, symbol, color, cmd):
        btn = tk.Label(parent, text=symbol, font=("Segoe UI", 13, "bold"),
                       fg=TEXT_DIM, bg=BG_MAIN, width=3, cursor="hand2")
        btn.pack(side="right")
        btn.bind("<Enter>", lambda e: btn.configure(fg=color))
        btn.bind("<Leave>", lambda e: btn.configure(fg=TEXT_DIM))
        btn.bind("<Button-1>", lambda e: cmd())
        return btn

    # ── Send Panel ─────────────────────────────────────────────────────────────
    def _build_send_panel(self):
        self.send_panel = tk.Frame(self.content, bg=BG_CARD)

        # Drop zone
        self.drop_canvas = tk.Canvas(self.send_panel, width=340, height=140,
                                     bg=BG_SURFACE, bd=0, highlightthickness=0)
        self.drop_canvas.pack(fill="x", pady=(4, 8))
        self._draw_drop_zone(hover=False)

        if DND_AVAILABLE:
            self.drop_canvas.drop_target_register(DND_FILES)
            self.drop_canvas.dnd_bind("<<Drop>>", self._on_drop)
            self.drop_canvas.dnd_bind("<<DragEnter>>", self._on_drag_enter)
            self.drop_canvas.dnd_bind("<<DragLeave>>", self._on_drag_leave)
        else:
            self.drop_canvas.bind("<Button-1>", self._browse_files)

        self.drop_canvas.bind("<Button-1>", self._browse_files)

        # File list
        list_frame = tk.Frame(self.send_panel, bg=BG_CARD)
        list_frame.pack(fill="x", pady=(0, 6))

        tk.Label(list_frame, text="Files đã chọn:", font=FONT_SMALL,
                 fg=TEXT_MUTED, bg=BG_CARD).pack(anchor="w")

        self.file_listbox = tk.Listbox(list_frame, bg=BG_SURFACE, fg=TEXT,
                                       font=FONT_SMALL, bd=0, highlightthickness=1,
                                       highlightcolor=ACCENT_DIM,
                                       selectbackground=ACCENT_DIM,
                                       selectforeground=TEXT,
                                       height=4, activestyle="none")
        self.file_listbox.pack(fill="x", pady=2)

        list_actions = tk.Frame(list_frame, bg=BG_CARD)
        list_actions.pack(fill="x")
        self._small_btn(list_actions, "➕ Thêm file", self._browse_files).pack(side="left", padx=(0, 4))
        self._small_btn(list_actions, "🗑 Xóa chọn", self._remove_selected).pack(side="left", padx=(0, 4))
        self._small_btn(list_actions, "✖ Xóa tất cả", self._clear_files).pack(side="left")

        # Code phrase option
        code_frame = tk.Frame(self.send_panel, bg=BG_CARD)
        code_frame.pack(fill="x", pady=(4, 8))
        tk.Label(code_frame, text="Mã tùy chỉnh (để trống = tự động):",
                 font=FONT_SMALL, fg=TEXT_MUTED, bg=BG_CARD).pack(anchor="w")
        self.custom_code_var = tk.StringVar()
        code_entry = tk.Entry(code_frame, textvariable=self.custom_code_var,
                              font=FONT_MONO, bg=BG_SURFACE, fg=TEXT, bd=0,
                              insertbackground=ACCENT, highlightthickness=1,
                              highlightcolor=ACCENT, highlightbackground=BORDER)
        code_entry.pack(fill="x", pady=2, ipady=5, padx=1)

        # Send button
        self.send_btn = self._action_btn(self.send_panel, "📤  Gửi File", ACCENT, self._send_files)
        self.send_btn.pack(fill="x", pady=(0, 4))

        # Cancel button (hidden by default)
        self.cancel_btn = self._action_btn(self.send_panel, "⛔  Hủy", RED_DIM, self._cancel_send)
        self.cancel_btn.pack(fill="x", pady=(0, 4))
        self.cancel_btn.pack_forget()

        # Progress / code display
        self.code_frame = tk.Frame(self.send_panel, bg=GREEN_DIM, padx=8, pady=6)
        self.code_frame.pack(fill="x")
        toplbl = tk.Label(self.code_frame, text="Mã nhận file:", font=FONT_SMALL,
                          fg=GREEN, bg=GREEN_DIM)
        toplbl.pack(anchor="w")
        self.code_display = tk.Label(self.code_frame, text="", font=("Consolas", 14, "bold"),
                                     fg=GREEN, bg=GREEN_DIM, cursor="hand2")
        self.code_display.pack(anchor="w")
        copy_btn = tk.Label(self.code_frame, text="📋 Sao chép", font=FONT_SMALL,
                            fg=GREEN, bg=GREEN_DIM, cursor="hand2")
        copy_btn.pack(anchor="w")
        copy_btn.bind("<Button-1>", self._copy_code)
        self.code_frame.pack_forget()

    # ── Receive Panel ──────────────────────────────────────────────────────────
    def _build_receive_panel(self):
        self.recv_panel = tk.Frame(self.content, bg=BG_CARD)

        tk.Label(self.recv_panel, text="Nhập mã để nhận file:",
                 font=FONT_BODY, fg=TEXT, bg=BG_CARD).pack(anchor="w", pady=(8, 4))

        self.recv_code_var = tk.StringVar()
        recv_entry = tk.Entry(self.recv_panel, textvariable=self.recv_code_var,
                              font=("Consolas", 14), bg=BG_SURFACE, fg=GREEN, bd=0,
                              insertbackground=GREEN, highlightthickness=1,
                              highlightcolor=GREEN, highlightbackground=BORDER)
        recv_entry.pack(fill="x", ipady=8, padx=1)
        recv_entry.bind("<Return>", lambda e: self._receive_file())

        # Save directory
        savedir_frame = tk.Frame(self.recv_panel, bg=BG_CARD)
        savedir_frame.pack(fill="x", pady=(8, 4))
        tk.Label(savedir_frame, text="Lưu tại thư mục:", font=FONT_SMALL,
                 fg=TEXT_MUTED, bg=BG_CARD).pack(anchor="w")

        dir_row = tk.Frame(savedir_frame, bg=BG_CARD)
        dir_row.pack(fill="x")
        self.save_dir_var = tk.StringVar(value=os.path.expanduser("~/Downloads"))
        dir_entry = tk.Entry(dir_row, textvariable=self.save_dir_var,
                             font=FONT_SMALL, bg=BG_SURFACE, fg=TEXT, bd=0,
                             insertbackground=ACCENT, highlightthickness=1,
                             highlightcolor=ACCENT, highlightbackground=BORDER)
        dir_entry.pack(side="left", fill="x", expand=True, ipady=5, padx=(1, 4))
        self._small_btn(dir_row, "📁", self._browse_save_dir).pack(side="left")

        # Overwrite option
        self.overwrite_var = tk.BooleanVar(value=False)
        ov_cb = tk.Checkbutton(self.recv_panel, text="Ghi đè file đã có",
                               variable=self.overwrite_var,
                               font=FONT_SMALL, fg=TEXT_MUTED, bg=BG_CARD,
                               activebackground=BG_CARD, activeforeground=TEXT,
                               selectcolor=BG_SURFACE, bd=0, cursor="hand2")
        ov_cb.pack(anchor="w", pady=4)

        self.recv_btn = self._action_btn(self.recv_panel, "📥  Nhận File", GREEN, self._receive_file)
        self.recv_btn.pack(fill="x", pady=(8, 4))

        self.recv_cancel_btn = self._action_btn(self.recv_panel, "⛔  Hủy", RED_DIM, self._cancel_recv)
        self.recv_cancel_btn.pack(fill="x")
        self.recv_cancel_btn.pack_forget()

    # ── Tab switching ──────────────────────────────────────────────────────────
    def _switch_tab(self, tab):
        self.active_tab.set(tab)
        for t, btn in self.tab_btns.items():
            if t == tab:
                btn.configure(fg=ACCENT_LIGHT,
                              font=("Segoe UI", 10, "bold"))
            else:
                btn.configure(fg=TEXT_MUTED, font=FONT_BODY)
        if tab == "send":
            self.recv_panel.pack_forget()
            self.send_panel.pack(fill="both", expand=True)
        else:
            self.send_panel.pack_forget()
            self.recv_panel.pack(fill="both", expand=True)

    # ── Drop zone drawing ─────────────────────────────────────────────────────
    def _draw_drop_zone(self, hover=False):
        c = self.drop_canvas
        c.delete("all")
        w, h = 340, 140
        bg = BG_HOVER if hover else BG_SURFACE
        border_color = ACCENT if hover else BORDER
        c.configure(bg=bg)

        # Dashed border (simulated)
        dash = 6
        for i in range(0, w, dash * 2):
            c.create_line(i, 0, min(i + dash, w), 0, fill=border_color, width=2)
            c.create_line(i, h, min(i + dash, w), h, fill=border_color, width=2)
        for i in range(0, h, dash * 2):
            c.create_line(0, i, 0, min(i + dash, h), fill=border_color, width=2)
            c.create_line(w, i, w, min(i + dash, h), fill=border_color, width=2)

        icon = "🐊" if not hover else "📂"
        c.create_text(w // 2, h // 2 - 22, text=icon, font=("Segoe UI", 28),
                      fill=ACCENT if hover else TEXT_DIM)
        c.create_text(w // 2, h // 2 + 14,
                      text="Kéo & thả file vào đây" if not hover else "Thả file!",
                      font=("Segoe UI", 11, "bold"),
                      fill=ACCENT_LIGHT if hover else TEXT_MUTED)
        c.create_text(w // 2, h // 2 + 35,
                      text="hoặc click để chọn",
                      font=FONT_SMALL, fill=TEXT_DIM)

    # ── Drag & Drop callbacks ─────────────────────────────────────────────────
    def _on_drag_enter(self, event):
        self._draw_drop_zone(hover=True)

    def _on_drag_leave(self, event):
        self._draw_drop_zone(hover=False)

    def _on_drop(self, event):
        self._draw_drop_zone(hover=False)
        paths = self.root.tk.splitlist(event.data)
        for p in paths:
            p = p.strip("{}")
            if p and p not in self.queued_files:
                self.queued_files.append(p)
                self.file_listbox.insert("end", os.path.basename(p))
        self._log(f"📂 Đã thêm {len(paths)} file(s)", "info")

    # ── File browsing ──────────────────────────────────────────────────────────
    def _browse_files(self, event=None):
        from tkinter import filedialog
        paths = filedialog.askopenfilenames(title="Chọn file để gửi")
        for p in paths:
            if p and p not in self.queued_files:
                self.queued_files.append(p)
                self.file_listbox.insert("end", os.path.basename(p))

    def _browse_save_dir(self):
        from tkinter import filedialog
        d = filedialog.askdirectory(title="Chọn thư mục lưu file")
        if d:
            self.save_dir_var.set(d)

    def _remove_selected(self):
        sel = list(self.file_listbox.curselection())
        for i in reversed(sel):
            self.file_listbox.delete(i)
            del self.queued_files[i]

    def _clear_files(self):
        self.file_listbox.delete(0, "end")
        self.queued_files.clear()

    # ── Send logic ─────────────────────────────────────────────────────────────
    def _send_files(self):
        if not self.croc_path:
            messagebox.showerror("Lỗi", "Không tìm thấy croc!\nVui lòng cài croc và thêm vào PATH.")
            return
        if not self.queued_files:
            messagebox.showwarning("Chưa chọn file", "Vui lòng kéo thả hoặc chọn file trước.")
            return

        self.code_frame.pack_forget()
        self.code_display.configure(text="")
        self.send_btn.pack_forget()
        self.cancel_btn.pack(fill="x", pady=(0, 4))
        self._log("🚀 Đang gửi file...", "info")

        cmd = [self.croc_path, "send"]
        code = self.custom_code_var.get().strip()
        if code:
            cmd += ["--code", code]
        cmd += self.queued_files

        def run():
            try:
                self.send_process = subprocess.Popen(
                    cmd,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    creationflags=subprocess.CREATE_NO_WINDOW if sys.platform == "win32" else 0,
                )
                code_found = False
                for line in self.send_process.stdout:
                    line = line.rstrip()
                    if not line:
                        continue
                    self._log(line, "info")
                    # Extract code phrase
                    m = re.search(r"Code is:\s*(.+)", line)
                    if m:
                        phrase = m.group(1).strip()
                        self.root.after(0, self._show_code, phrase)
                        code_found = True
                self.send_process.wait()
                rc = self.send_process.returncode
                if rc == 0:
                    self._log("✅ Gửi thành công!", "success")
                else:
                    self._log(f"❌ Kết thúc với mã lỗi: {rc}", "error")
            except Exception as ex:
                self._log(f"❌ Lỗi: {ex}", "error")
            finally:
                self.root.after(0, self._send_done)

        threading.Thread(target=run, daemon=True).start()

    def _show_code(self, phrase):
        self.code_display.configure(text=phrase)
        self._current_code = phrase
        self.code_frame.pack(fill="x", pady=4)
        # Copy to clipboard
        self.root.clipboard_clear()
        self.root.clipboard_append(phrase)
        self._log(f"📋 Code: {phrase} (đã sao chép)", "code")

    def _copy_code(self, event=None):
        code = self.code_display.cget("text")
        if code:
            self.root.clipboard_clear()
            self.root.clipboard_append(code)
            self._log("📋 Đã sao chép mã!", "success")

    def _cancel_send(self):
        if self.send_process and self.send_process.poll() is None:
            self.send_process.terminate()
            self._log("⛔ Đã hủy gửi file.", "warn")
        self._send_done()

    def _send_done(self):
        self.cancel_btn.pack_forget()
        self.send_btn.pack(fill="x", pady=(0, 4))
        self.send_process = None

    # ── Receive logic ──────────────────────────────────────────────────────────
    def _receive_file(self):
        if not self.croc_path:
            messagebox.showerror("Lỗi", "Không tìm thấy croc!")
            return
        code = self.recv_code_var.get().strip()
        if not code:
            messagebox.showwarning("Thiếu mã", "Vui lòng nhập mã nhận file.")
            return

        save_dir = self.save_dir_var.get().strip()
        os.makedirs(save_dir, exist_ok=True)

        self.recv_btn.pack_forget()
        self.recv_cancel_btn.pack(fill="x")
        self._log(f"📥 Đang nhận file với mã: {code}", "info")

        cmd = [self.croc_path, "--yes"]
        if self.overwrite_var.get():
            cmd.append("--overwrite")
        cmd.append(code)

        def run():
            try:
                self.recv_process = subprocess.Popen(
                    cmd,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    cwd=save_dir,
                    creationflags=subprocess.CREATE_NO_WINDOW if sys.platform == "win32" else 0,
                )
                for line in self.recv_process.stdout:
                    self._log(line.rstrip(), "info")
                self.recv_process.wait()
                rc = self.recv_process.returncode
                if rc == 0:
                    self._log(f"✅ Nhận file xong! Lưu tại: {save_dir}", "success")
                else:
                    self._log(f"❌ Lỗi nhận file (code {rc})", "error")
            except Exception as ex:
                self._log(f"❌ {ex}", "error")
            finally:
                self.root.after(0, self._recv_done)

        threading.Thread(target=run, daemon=True).start()
        self.recv_process = None

    def _cancel_recv(self):
        if hasattr(self, "recv_process") and self.recv_process and self.recv_process.poll() is None:
            self.recv_process.terminate()
            self._log("⛔ Đã hủy nhận file.", "warn")
        self._recv_done()

    def _recv_done(self):
        self.recv_cancel_btn.pack_forget()
        self.recv_btn.pack(fill="x", pady=(8, 4))

    # ── Log ───────────────────────────────────────────────────────────────────
    def _log(self, msg, tag="info"):
        def _do():
            self.log_text.configure(state="normal")
            self.log_text.insert("end", msg + "\n", tag)
            self.log_text.see("end")
            self.log_text.configure(state="disabled")
        self.root.after(0, _do)

    def _clear_log(self):
        self.log_text.configure(state="normal")
        self.log_text.delete("1.0", "end")
        self.log_text.configure(state="disabled")

    # ── Window controls ───────────────────────────────────────────────────────
    def _toggle_topmost(self, event=None):
        current = self.root.wm_attributes("-topmost")
        self.root.wm_attributes("-topmost", not current)
        self.pin_btn.configure(fg=ACCENT if not current else TEXT_DIM)

    def _minimize(self):
        self.root.overrideredirect(False)
        self.root.iconify()
        def restore_check():
            if self.root.state() == "normal":
                self.root.overrideredirect(True)
            else:
                self.root.after(200, restore_check)
        self.root.after(200, restore_check)

    def _quit(self):
        if self.send_process and self.send_process.poll() is None:
            self.send_process.terminate()
        self.root.quit()

    # ── Dragging ──────────────────────────────────────────────────────────────
    def _start_drag(self, event):
        self._offset_x = event.x_root - self.root.winfo_x()
        self._offset_y = event.y_root - self.root.winfo_y()

    def _do_drag(self, event):
        x = event.x_root - self._offset_x
        y = event.y_root - self._offset_y
        self.root.geometry(f"+{x}+{y}")

    # ── Widget factories ──────────────────────────────────────────────────────
    def _small_btn(self, parent, text, cmd=None):
        btn = tk.Label(parent, text=text, font=FONT_SMALL, fg=ACCENT,
                       bg=BG_SURFACE, cursor="hand2", padx=6, pady=3,
                       relief="flat", bd=0)
        if cmd:
            btn.bind("<Button-1>", lambda e: cmd())
        btn.bind("<Enter>", lambda e: btn.configure(bg=BG_HOVER, fg=ACCENT_LIGHT))
        btn.bind("<Leave>", lambda e: btn.configure(bg=BG_SURFACE, fg=ACCENT))
        return btn

    def _action_btn(self, parent, text, color, cmd):
        frame = tk.Frame(parent, bg=color, cursor="hand2")
        lbl = tk.Label(frame, text=text, font=("Segoe UI", 11, "bold"),
                       fg=TEXT, bg=color, pady=10, cursor="hand2")
        lbl.pack(expand=True)
        frame.bind("<Button-1>", lambda e: cmd())
        lbl.bind("<Button-1>", lambda e: cmd())
        frame.bind("<Enter>", lambda e: frame.configure(bg=self._lighten(color)))
        frame.bind("<Leave>", lambda e: frame.configure(bg=color))
        lbl.bind("<Enter>", lambda e: lbl.configure(bg=self._lighten(color)))
        lbl.bind("<Leave>", lambda e: lbl.configure(bg=color))
        return frame

    def _lighten(self, hex_color):
        try:
            r = int(hex_color[1:3], 16)
            g = int(hex_color[3:5], 16)
            b = int(hex_color[5:7], 16)
            r = min(255, r + 30)
            g = min(255, g + 30)
            b = min(255, b + 30)
            return f"#{r:02x}{g:02x}{b:02x}"
        except Exception:
            return hex_color

    def run(self):
        self.root.mainloop()


# ─── Entry point ──────────────────────────────────────────────────────────────
if __name__ == "__main__":
    import tkinter.font   # noqa – ensure font module is loaded
    app = CrocGUI()
    app.run()
