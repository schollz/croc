package main

import (
	"fmt"
	"os"
	"time"

	"github.com/schollz/croc/src/cli"
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
	window.SetFixedSize2(250, 200)
	window.SetWindowTitle("croc - secure data transfer")

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
	labels[0].SetText("Click 'Send' or 'Receive' to start")

	button := widgets.NewQPushButton2("Send file", nil)
	button.ConnectClicked(func(bool) {
		if isWorking {
			var info = widgets.NewQMessageBox(nil)
			info.SetWindowTitle("Info")
			info.SetText(fmt.Sprintf("Can only do one send or recieve at a time"))
			info.Exec()
			return
		}
		isWorking = true

		var fileDialog = widgets.NewQFileDialog2(nil, "Open file to send...", "", "")
		fileDialog.SetAcceptMode(widgets.QFileDialog__AcceptOpen)
		fileDialog.SetFileMode(widgets.QFileDialog__AnyFile)
		if fileDialog.Exec() != int(widgets.QDialog__Accepted) {
			return
		}
		var fn = fileDialog.SelectedFiles()[0]
		fmt.Println(fn)
		for i, label := range labels {
			go func(i int, label *CustomLabel) {
				var tick int
				for range time.NewTicker(time.Duration((i+1)*25) * time.Millisecond).C {
					tick++
					label.SetText(fmt.Sprintf("%v %v", tick, time.Now().UTC().Format("15:04:05.0000")))
				}
			}(i, label)
		}
	})
	widget.Layout().AddWidget(button)

	receiveButton := widgets.NewQPushButton2("Receive", nil)
	receiveButton.ConnectClicked(func(bool) {
		if isWorking {
			var info = widgets.NewQMessageBox(nil)
			info.SetWindowTitle("Info")
			info.SetText(fmt.Sprintf("Can only do one send or recieve at a time"))
			info.Exec()
			return
		}
		isWorking = true
		var codePhrase = widgets.QInputDialog_GetText(nil, "Enter code phrase", "",
			widgets.QLineEdit__Normal, "", true, core.Qt__Dialog, core.Qt__ImhNone)
		fmt.Println(codePhrase)
		var folderDialog = widgets.NewQFileDialog2(nil, "Open folder to receive file...", "", "")
		folderDialog.SetAcceptMode(widgets.QFileDialog__AcceptOpen)
		folderDialog.SetFileMode(widgets.QFileDialog__DirectoryOnly)
		if folderDialog.Exec() != int(widgets.QDialog__Accepted) {
			return
		}
		var fn = folderDialog.SelectedFiles()[0]
		fmt.Println(fn)
		for i, label := range labels {
			go func(i int, label *CustomLabel) {
				var tick int
				for range time.NewTicker(time.Duration((i+1)*25) * time.Millisecond).C {
					tick++
					label.SetText(fmt.Sprintf("%v %v", tick, time.Now().UTC().Format("15:04:05.0000")))
				}
			}(i, label)
		}
	})
	widget.Layout().AddWidget(receiveButton)

	window.Show()
	app.Exec()
}
