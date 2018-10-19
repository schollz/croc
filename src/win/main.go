// +build wincroc

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/schollz/croc/src/cli"
	"github.com/schollz/croc/src/croc"
	"github.com/schollz/croc/src/utils"
	"github.com/skratchdot/open-golang/open"
	"github.com/therecipe/qt/core"
	"github.com/therecipe/qt/widgets"
)

type CustomLabel struct {
	widgets.QLabel

	_ func(string) `signal:"updateTextFromGoroutine,auto(this.QLabel.setText)"` //TODO: support this.setText as well
}

var Version string

func main() {
	if len(os.Args) > 1 {
		cli.Run()
		return
	}

	var isWorking bool
	app := widgets.NewQApplication(len(os.Args), os.Args)

	window := widgets.NewQMainWindow(nil, 0)
	window.SetFixedSize2(400, 150)
	window.SetWindowTitle("🐊📦 croc " + Version)

	widget := widgets.NewQWidget(nil, 0)
	widget.SetLayout(widgets.NewQVBoxLayout())
	window.SetCentralWidget(widget)

	labels := make([]*CustomLabel, 3)
	for i := range labels {
		label := NewCustomLabel(nil, 0)
		label.SetAlignment(core.Qt__AlignCenter)
		widget.Layout().AddWidget(label)
		labels[i] = label
	}
	labels[0].SetText("secure data transfer")
	labels[1].SetText("Click 'Send' or 'Receive' to start")

	button := widgets.NewQPushButton2("Send", nil)
	button.ConnectClicked(func(bool) {
		if isWorking {
			dialog("Can only do one send or receive at a time")
			return
		}
		isWorking = true

		var fileDialog = widgets.NewQFileDialog2(window, "Open file to send...", "", "")
		fileDialog.SetAcceptMode(widgets.QFileDialog__AcceptOpen)
		fileDialog.SetFileMode(widgets.QFileDialog__AnyFile)
		if fileDialog.Exec() != int(widgets.QDialog__Accepted) {
			isWorking = false
			return
		}
		var fn = fileDialog.SelectedFiles()[0]
		if len(fn) == 0 {
			dialog(fmt.Sprintf("No file selected"))
			isWorking = false
			return
		}

		go func() {
			cr := croc.Init(false)
			done := make(chan bool)
			codePhrase := utils.GetRandomName()
			_, fname := filepath.Split(fn)
			labels[0].UpdateTextFromGoroutine(fmt.Sprintf("Sending '%s'", fname))
			labels[1].UpdateTextFromGoroutine(fmt.Sprintf("Code phrase: %s", codePhrase))

			go func(done chan bool) {
				for {
					if cr.OtherIP != "" && cr.FileInfo.SentName != "" {
						bytesString := humanize.Bytes(uint64(cr.FileInfo.Size))
						fileOrFolder := "file"
						if cr.FileInfo.IsDir {
							fileOrFolder = "folder"
						}
						labels[0].UpdateTextFromGoroutine(fmt.Sprintf("Sending %s %s '%s' to %s", bytesString, fileOrFolder, cr.FileInfo.SentName, cr.OtherIP))
					}
					if cr.Bar != nil {
						barState := cr.Bar.State()
						labels[1].UpdateTextFromGoroutine(fmt.Sprintf("%2.1f%% [%2.0fs:%2.0fs]", barState.CurrentPercent*100, barState.SecondsSince, barState.SecondsLeft))
					}
					labels[2].UpdateTextFromGoroutine(cr.StateString)
					time.Sleep(100 * time.Millisecond)
					select {
					case _ = <-done:
						labels[2].UpdateTextFromGoroutine(cr.StateString)
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
	widget.Layout().AddWidget(button)

	receiveButton := widgets.NewQPushButton2("Receive", nil)
	receiveButton.ConnectClicked(func(bool) {
		if isWorking {
			dialog("Can only do one send or receive at a time")
			return
		}
		labels[1].SetText("")
		labels[2].SetText("please wait...")
		isWorking = true
		defer func() {
			isWorking = false
		}()

		// determine the folder to save the file
		var folderDialog = widgets.NewQFileDialog2(window, "Open folder to receive file...", "", "")
		folderDialog.SetAcceptMode(widgets.QFileDialog__AcceptOpen)
		folderDialog.SetFileMode(widgets.QFileDialog__DirectoryOnly)
		if folderDialog.Exec() != int(widgets.QDialog__Accepted) {
			return
		}
		var fn = folderDialog.SelectedFiles()[0]
		if len(fn) == 0 {
			labels[2].SetText(fmt.Sprintf("No folder selected"))
			return
		}

		var codePhrase = widgets.QInputDialog_GetText(window, "croc", "Enter code phrase:",
			widgets.QLineEdit__Normal, "", true, core.Qt__Dialog, core.Qt__ImhNone)
		if len(codePhrase) < 3 {
			labels[2].SetText(fmt.Sprintf("Invalid codephrase: '%s'", codePhrase))
			return
		}

		// change into the receiving directory
		cwd, _ := os.Getwd()
		defer os.Chdir(cwd)
		os.Chdir(fn)

		cr := croc.Init(true)
		cr.WindowRecipientPrompt = true

		done := make(chan bool)
		go func() {
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
					labels[1].UpdateTextFromGoroutine(fmt.Sprintf("%2.1f%% [%2.0fs:%2.0fs]", barState.CurrentPercent*100, barState.SecondsSince, barState.SecondsLeft))
				}
				if cr.StateString != "" {
					labels[2].UpdateTextFromGoroutine(cr.StateString)
				}
				time.Sleep(100 * time.Millisecond)
				select {
				case _ = <-done:
					labels[2].UpdateTextFromGoroutine(cr.StateString)
					return
				default:
					continue
				}
			}
		}()

		for {
			if cr.WindowReceivingString != "" {
				var question = widgets.QMessageBox_Question(window, "croc", fmt.Sprintf("%s?", cr.WindowReceivingString), widgets.QMessageBox__Yes|widgets.QMessageBox__No, 0)
				if question == widgets.QMessageBox__Yes {
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
	widget.Layout().AddWidget(receiveButton)

	window.Show()
	app.Exec()
}

func dialog(s string) {
	var info = widgets.NewQMessageBox(nil)
	info.SetWindowTitle("Info")
	info.SetText(s)
	info.Exec()
}
