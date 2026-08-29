package croc

import (
	"strings"
	"testing"
)

func TestSendingTextOfferRejectsDisguisedFileTransfers(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*SenderInfo)
	}{
		{name: "arbitrary filename", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].Name = ".bashrc"
		}},
		{name: "git hook", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].Name = "post-checkout"
			info.FilesToTransfer[0].FolderRemote = ".git/hooks"
		}},
		{name: "extractable archive", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].TempFile = true
		}},
		{name: "symlink", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].Symlink = "target"
		}},
		{name: "nested folder", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].FolderRemote = "safe"
		}},
		{name: "empty payload", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].Size = 0
		}},
		{name: "oversized payload", modify: func(info *SenderInfo) {
			info.FilesToTransfer[0].Size = maxTextTransferBytes + 1
		}},
		{name: "second file", modify: func(info *SenderInfo) {
			info.FilesToTransfer = append(info.FilesToTransfer, FileInfo{Name: "croc-stdin-two", FolderRemote: ".", Size: 1})
		}},
		{name: "empty folder", modify: func(info *SenderInfo) {
			info.EmptyFoldersToTransfer = []FileInfo{{FolderRemote: "empty"}}
			info.TotalNumberFolders = 1
		}},
		{name: "reported folder count", modify: func(info *SenderInfo) {
			info.TotalNumberFolders = 1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := SenderInfo{
				FilesToTransfer: []FileInfo{{Name: "croc-stdin-one", FolderRemote: ".", Size: 1}},
				SendingText:     true,
			}
			tt.modify(&info)
			client := &Client{Options: Options{NoPrompt: true}}
			done, err := client.processSenderInfo(info)
			if !done || err == nil {
				t.Fatalf("processSenderInfo() = (%v, %v), want rejection", done, err)
			}
			if client.Options.SendingText || client.Options.Stdout {
				t.Fatal("rejected offer enabled text or stdout mode")
			}
		})
	}
}

func TestSendingTextOfferAcceptsHonestPayload(t *testing.T) {
	t.Chdir(t.TempDir())
	client := &Client{Options: Options{NoPrompt: true}}
	defer client.closeReceiveFilesystem()
	info := SenderInfo{
		FilesToTransfer: []FileInfo{{Name: "croc-stdin-honest", FolderRemote: ".", Size: 12}},
		SendingText:     true,
	}

	done, err := client.processSenderInfo(info)
	if done || err != nil {
		t.Fatalf("processSenderInfo() = (%v, %v), want accepted offer", done, err)
	}
	if !client.Options.SendingText || !client.Options.Stdout {
		t.Fatal("honest text offer did not enable text output")
	}
	if len(client.FilesToTransfer) != 1 || !strings.Contains(client.FilesToTransfer[0].Name, "croc-stdin-") {
		t.Fatalf("received text metadata = %+v", client.FilesToTransfer)
	}
}
