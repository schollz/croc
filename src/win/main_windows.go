package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/gonutz/w32"
	"github.com/gonutz/wui"
	"github.com/schollz/croc/src/cli"
	"github.com/schollz/croc/src/croc"
	"github.com/schollz/croc/src/utils"
	"github.com/skratchdot/open-golang/open"
)

var Version string

func main() {
	if len(os.Args) > 1 {
		cli.Run()
		return
	}

	var isWorking bool

	font, _ := wui.NewFont(wui.FontDesc{
		Name:   "Tahoma",
		Height: -11,
	})

	window := wui.NewWindow()
	window.SetFont(font)
	window.SetIconFromMem(icon)
	window.SetStyle(w32.WS_OVERLAPPED | w32.WS_CAPTION | w32.WS_SYSMENU | w32.WS_MINIMIZEBOX)
	if runtime.GOOS == "windows" {
		window.SetClientSize(300, 150)
		window.SetTitle("croc " + Version)
	} else {
		window.SetClientSize(400, 150)
		window.SetTitle("🐊📦 croc " + Version)
	}

	labels := make([]*wui.Label, 3)
	for i := range labels {
		label := wui.NewLabel()
		label.SetCenterAlign()
		label.SetBounds(0, 10+i*20, window.ClientWidth(), 20)
		window.Add(label)
		labels[i] = label
	}
	labels[0].SetText("secure data transfer")
	labels[1].SetText("Click 'Send' or 'Receive' to start")

	button := wui.NewButton()
	button.SetText("Send")
	window.Add(button)
	button.SetBounds(10, window.ClientHeight()-70, window.ClientWidth()-20, 25)
	button.SetOnClick(func() {
		if isWorking {
			wui.MessageBoxError(window, "Error", "Can only do one send or receive at a time")
			return
		}
		isWorking = true

		fileDialog := wui.NewFileOpenDialog()
		fileDialog.SetTitle("Open file to send...")
		accepted, fn := fileDialog.ExecuteSingleSelection(window)
		if !accepted {
			isWorking = false
			return
		}
		if fn == "" {
			wui.MessageBoxError(window, "Error", "No file selected")
			isWorking = false
			return
		}

		go func() {
			cr := croc.Init(false)
			done := make(chan bool)
			codePhrase := utils.GetRandomName()
			_, fname := filepath.Split(fn)
			labels[0].SetText(fmt.Sprintf("Sending '%s'", fname))
			labels[1].SetText(fmt.Sprintf("Code phrase: %s", codePhrase))

			go func(done chan bool) {
				for {
					if cr.OtherIP != "" && cr.FileInfo.SentName != "" {
						bytesString := humanize.Bytes(uint64(cr.FileInfo.Size))
						fileOrFolder := "file"
						if cr.FileInfo.IsDir {
							fileOrFolder = "folder"
						}
						labels[0].SetText(fmt.Sprintf("Sending %s %s '%s' to %s", bytesString, fileOrFolder, cr.FileInfo.SentName, cr.OtherIP))
					}
					if cr.Bar != nil {
						barState := cr.Bar.State()
						labels[1].SetText(fmt.Sprintf("%2.1f%% [%2.0fs:%2.0fs]", barState.CurrentPercent*100, barState.SecondsSince, barState.SecondsLeft))
					}
					labels[2].SetText(cr.StateString)
					time.Sleep(100 * time.Millisecond)
					select {
					case _ = <-done:
						labels[2].SetText(cr.StateString)
						return
					default:
						continue
					}
				}
			}(done)

			cr.Send(fn, codePhrase)
			done <- true
			isWorking = false
		}()
	})

	receiveButton := wui.NewButton()
	receiveButton.SetText("Receive")
	window.Add(receiveButton)
	receiveButton.SetBounds(10, window.ClientHeight()-35, window.ClientWidth()-20, 25)
	receiveButton.SetOnClick(func() {
		if isWorking {
			wui.MessageBoxError(window, "Error", "Can only do one send or receive at a time")
			return
		}
		labels[1].SetText("")
		labels[2].SetText("please wait...")
		isWorking = true
		defer func() {
			isWorking = false
		}()

		// determine the folder to save the file
		folderDialog := wui.NewFolderSelectDialog()
		folderDialog.SetTitle("Open folder to receive file...")
		accepted, fn := folderDialog.Execute(window)
		if !accepted {
			return
		}
		if len(fn) == 0 {
			labels[2].SetText(fmt.Sprintf("No folder selected"))
			return
		}

		passDlg := wui.NewDialogWindow()
		passDlg.SetTitle("Enter code phrase")
		passDlg.SetClientSize(window.ClientWidth()-20, 40)
		pass := wui.NewEditLine()
		passDlg.Add(pass)
		pass.SetPassword(true)
		pass.SetBounds(10, 10, passDlg.ClientWidth()-20, 20)
		var passAccepted bool
		passDlg.SetShortcut(wui.ShortcutKeys{Key: w32.VK_RETURN}, func() {
			passAccepted = true
			passDlg.Close()
		})
		passDlg.SetShortcut(wui.ShortcutKeys{Key: w32.VK_ESCAPE}, func() {
			passDlg.Close()
		})
		var codePhrase string
		passDlg.SetOnShow(func() {
			passDlg.SetX(window.X() + (window.Width()-passDlg.Width())/2)
			passDlg.SetY(window.Y() + (window.Height()-passDlg.Height())/2)
			pass.Focus()
		})
		passDlg.SetOnClose(func() {
			if passAccepted {
				codePhrase = pass.Text()
			}
		})
		passDlg.ShowModal(window)
		passDlg.Destroy()
		if len(codePhrase) < 3 {
			labels[2].SetText(fmt.Sprintf("Invalid codephrase: '%s'", codePhrase))
			return
		}

		cr := croc.Init(false)
		cr.WindowRecipientPrompt = true

		done := make(chan bool)
		go func() {
			// change into the receiving directory
			cwd, _ := os.Getwd()
			defer os.Chdir(cwd)
			os.Chdir(fn)
			err := cr.Receive(codePhrase)
			if err == nil {
				open.Run(fn)
			}
			done <- true
			done <- true
			isWorking = false
		}()
		go func() {
			for {
				if cr.Bar != nil {
					barState := cr.Bar.State()
					labels[1].SetText(fmt.Sprintf("%2.1f%% [%2.0fs:%2.0fs]", barState.CurrentPercent*100, barState.SecondsSince, barState.SecondsLeft))
				}
				if cr.StateString != "" {
					labels[2].SetText(cr.StateString)
				}
				time.Sleep(100 * time.Millisecond)
				select {
				case _ = <-done:
					labels[2].SetText(cr.StateString)
					return
				default:
					continue
				}
			}
		}()

		for {
			if cr.WindowReceivingString != "" {
				question := wui.MessageBoxYesNo(
					window,
					"croc",
					fmt.Sprintf("%s?", cr.WindowReceivingString),
				)
				if question {
					cr.WindowRecipientAccept = true
					labels[0].SetText(cr.WindowReceivingString)
				} else {
					cr.WindowRecipientAccept = false
					labels[2].SetText("canceled")
				}
				cr.WindowRecipientPrompt = false
				cr.WindowReceivingString = ""
				break
			}
			time.Sleep(100 * time.Millisecond)
			select {
			case _ = <-done:
				labels[2].SetText(cr.StateString)
				return
			default:
				continue
			}
		}

	})

	window.Show()
}
