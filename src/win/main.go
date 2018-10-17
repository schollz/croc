// +build wincroc

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/schollz/croc/src/cli"
	"github.com/schollz/croc/src/croc"
	"github.com/schollz/croc/src/utils"
	"github.com/therecipe/qt/core"
	"github.com/therecipe/qt/widgets"
)

type CustomLabel struct {
	widgets.QLabel

	_ func(string) `signal:"updateTextFromGoroutine,auto(this.QLabel.setText)"` //TODO: support this.setText as well
}

func main() {
	if len(os.Args) > 1 {
		cli.Run()
		return
	}

	var isWorking bool
	app := widgets.NewQApplication(len(os.Args), os.Args)

	window := widgets.NewQMainWindow(nil, 0)
	window.SetFixedSize2(400, 150)
	window.SetWindowTitle("🐊📦 croc")

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

	button := widgets.NewQPushButton2("Send file", nil)
	button.ConnectClicked(func(bool) {
		if isWorking {
			dialog("Can only do one send or receive at a time")
			return
		}
		isWorking = true

		var fileDialog = widgets.NewQFileDialog2(nil, "Open file to send...", "", "")
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
					fmt.Println(cr.FileInfo, cr.Bar)
					if cr.FileInfo.SentName != "" {
						labels[0].UpdateTextFromGoroutine(fmt.Sprintf("Sending %s", cr.FileInfo.SentName))
					}
					if cr.Bar != nil {
						barState := cr.Bar.State()
						labels[1].UpdateTextFromGoroutine(fmt.Sprintf("%2.1f%% [%2.0f:%2.0f]", barState.CurrentPercent*100, barState.SecondsSince, barState.SecondsLeft))
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

		// for i, label := range labels {
		// 	go func(i int, label *CustomLabel) {
		// 		var tick int
		// 		for range time.NewTicker(time.Duration((i+1)*25) * time.Millisecond).C {
		// 			tick++
		// 			label.SetText(fmt.Sprintf("%v %v", tick, time.Now().UTC().Format("15:04:05.0000")))
		// 		}
		// 	}(i, label)
		// }
	})
	widget.Layout().AddWidget(button)

	receiveButton := widgets.NewQPushButton2("Receive", nil)
	receiveButton.ConnectClicked(func(bool) {
		if isWorking {
			dialog("Can only do one send or receive at a time")
			return
		}
		isWorking = true
		defer func() {
			isWorking = false
		}()

		var codePhrase = widgets.QInputDialog_GetText(nil, "Enter code phrase", "",
			widgets.QLineEdit__Normal, "", true, core.Qt__Dialog, core.Qt__ImhNone)
		if len(codePhrase) < 3 {
			dialog(fmt.Sprintf("Invalid codephrase: '%s'", codePhrase))
			return
		}
		var folderDialog = widgets.NewQFileDialog2(nil, "Open folder to receive file...", "", "")
		folderDialog.SetAcceptMode(widgets.QFileDialog__AcceptOpen)
		folderDialog.SetFileMode(widgets.QFileDialog__DirectoryOnly)
		if folderDialog.Exec() != int(widgets.QDialog__Accepted) {
			return
		}
		var fn = folderDialog.SelectedFiles()[0]
		if len(fn) == 0 {
			dialog(fmt.Sprintf("No folder selected"))
			return
		}
		cwd, _ := os.Getwd()
		os.Chdir(fn)
		defer os.Chdir(cwd)

		cr := croc.Init(true)
		done := make(chan bool)
		go func() {
			cr.Receive(codePhrase)
			done <- true
		}()

		for {
			select {
			case _ = <-done:
				break
			}
			labels[0].SetText(cr.StateString)
			if cr.FileInfo.SentName != "" {
				labels[0].SetText(fmt.Sprintf("%s", cr.FileInfo.SentName))
			}
			if cr.Bar != nil {
				barState := cr.Bar.State()
				labels[1].SetText(fmt.Sprintf("%2.1f", barState.CurrentPercent))
			}
			time.Sleep(100 * time.Millisecond)
		}

		// for i, label := range labels {
		// 	go func(i int, label *CustomLabel) {
		// 		var tick int
		// 		for range time.NewTicker(time.Duration((i+1)*25) * time.Millisecond).C {
		// 			tick++
		// 			label.SetText(fmt.Sprintf("%v %v", tick, time.Now().UTC().Format("15:04:05.0000")))
		// 		}
		// 	}(i, label)
		// }
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
