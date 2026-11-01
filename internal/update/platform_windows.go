package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

var kernel32 = windows.NewLazySystemDLL("kernel32.dll")
var lockFileEx = kernel32.NewProc("LockFileEx")
var replaceFileW = kernel32.NewProc("ReplaceFileW")
var shellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

func lockDirectory(directory string) (func(), error) {
	file, err := os.OpenFile(filepath.Join(directory, ".croc-update.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := &syscall.Overlapped{}
	ok, _, err := lockFileEx.Call(file.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
	if ok == 0 {
		file.Close()
		if errors.Is(err, syscall.Errno(33)) {
			return nil, errors.New("another croc installation is in progress; retry when it finishes")
		}
		return nil, err
	}
	return func() { file.Close() }, nil
}

func setMetadata(file *os.File, info os.FileInfo) error {
	mode := info.Mode().Perm()
	if info.IsDir() {
		mode = 0o755
	}
	return file.Chmod(mode)
}

func replaceFile(staged, target string, exists bool) error {
	if !exists {
		return os.Rename(staged, target)
	}
	if executableInUse(target) {
		return fmt.Errorf("executable is in use; close %s and retry", target)
	}
	backup, err := os.CreateTemp(filepath.Dir(target), ".croc-update-backup-*.exe")
	if err != nil {
		return err
	}
	backupName := backup.Name()
	backup.Close()
	if err := os.Remove(backupName); err != nil {
		return err
	}
	targetW, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	stagedW, err := syscall.UTF16PtrFromString(staged)
	if err != nil {
		return err
	}
	backupW, err := syscall.UTF16PtrFromString(backupName)
	if err != nil {
		return err
	}
	ok, _, callErr := replaceFileW.Call(uintptr(unsafe.Pointer(targetW)), uintptr(unsafe.Pointer(stagedW)), uintptr(unsafe.Pointer(backupW)), 0, 0, 0)
	if ok != 0 {
		_ = os.Remove(backupName)
		return nil
	}
	// ReplaceFile can move the original to its backup before a later failure.
	if _, err := os.Lstat(backupName); err == nil {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			if err := os.Rename(backupName, target); err != nil {
				return fmt.Errorf("%w; restore original executable from %s: %v", callErr, backupName, err)
			}
		} else {
			return fmt.Errorf("%w; original executable retained at %s", callErr, backupName)
		}
	}
	if errors.Is(callErr, syscall.Errno(32)) || errors.Is(callErr, syscall.Errno(1224)) || (errors.Is(callErr, windows.ERROR_ACCESS_DENIED) && executableInUse(target)) {
		// Do not classify a running-image lock as a permission failure: UAC
		// cannot resolve it, and the process must be allowed to finish.
		return fmt.Errorf("executable is in use; close %s and retry: %v", target, callErr)
	}
	return callErr
}

func executableInUse(target string) bool {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(target)) {
			continue
		}
		process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if err != nil {
			continue
		}
		buffer := make([]uint16, 32768)
		size := uint32(len(buffer))
		err = windows.QueryFullProcessImageName(process, 0, &buffer[0], &size)
		windows.CloseHandle(process)
		if err == nil && strings.EqualFold(filepath.Clean(windows.UTF16ToString(buffer[:size])), filepath.Clean(target)) {
			return true
		}
	}
	return false
}

type shellExecuteInfo struct {
	Size, Mask                        uint32
	Window                            uintptr
	Verb, File, Parameters, Directory *uint16
	Show                              int32
	Instance, IDList                  uintptr
	Class                             *uint16
	ClassKey                          uintptr
	HotKey                            uint32
	Icon                              uintptr
	Process                           syscall.Handle
}

func elevate(ctx context.Context, executable string, args []string) error {
	manual := "run in an Administrator PowerShell: " + ShellCommand(executable, publicUpdateArgs(args)...)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("interactive UAC authentication is unavailable; " + manual)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	parameters := ""
	for _, arg := range args {
		if parameters != "" {
			parameters += " "
		}
		parameters += syscall.EscapeArg(arg)
	}
	params, err := syscall.UTF16PtrFromString(parameters)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{Mask: 0x40 | 0x100, Verb: verb, File: file, Parameters: params, Show: 1}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, callErr := shellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		return fmt.Errorf("UAC elevation was denied or unavailable: %w; %s", callErr, manual)
	}
	if info.Process == 0 {
		return errors.New("UAC did not return an installer process; " + manual)
	}
	defer syscall.CloseHandle(info.Process)
	// Do not abandon an elevated installer once launched: its exit status is
	// needed to report whether installation actually completed.
	if _, err := syscall.WaitForSingleObject(info.Process, syscall.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := syscall.GetExitCodeProcess(info.Process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("elevated installer exited with code %d; %s", code, manual)
	}
	return nil
}
