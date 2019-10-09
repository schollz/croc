package main

import (
	"fmt"
	"image"
	"net/url"
	"os"
	"strings"
	"time"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	"github.com/schollz/croc/v6/src/croc"
	"github.com/schollz/croc/v6/src/models"
	"github.com/schollz/croc/v6/src/utils"
	nativedialog "github.com/sqweek/dialog"
)

var logoImage image.Image

func init() {
	infile, err := os.Open("croc.jpg")
	if err != nil {
		panic(err)
	}
	defer infile.Close()

	// Decode will figure out what type of image is in the file on its own.
	// We just have to be sure all the image packages we want are imported.
	logoImage, _, err = image.Decode(infile)
	if err != nil {
		panic(err)
	}
}

func welcomeScreen(a fyne.App) fyne.CanvasObject {
	logo := canvas.NewImageFromImage(logoImage)
	logo.SetMinSize(fyne.NewSize(256, 128))

	link, err := url.Parse("https://github.com/schollz/croc")
	if err != nil {
		fyne.LogError("Could not parse URL", err)
	}

	return widget.NewVBox(
		widget.NewLabelWithStyle("croc - securely send a file", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		widget.NewHBox(layout.NewSpacer(), logo, layout.NewSpacer()),
		widget.NewLabel(`Send files using a secure PAKE-encrypted
peer-to-peer connection.`),
		widget.NewHyperlinkWithStyle("help", link, fyne.TextAlignCenter, fyne.TextStyle{}),
		layout.NewSpacer(),
	)
}

func makeCell() fyne.CanvasObject {
	rect := canvas.NewRectangle(theme.BackgroundColor())
	rect.SetMinSize(fyne.NewSize(30, 30))
	return rect
}

func main() {
	a := app.New()
	a.Settings().SetTheme(theme.LightTheme())
	w := a.NewWindow("croc")
	w.Resize(fyne.Size{400, 200})

	progress := widget.NewProgressBar()
	var sendFileButton *widget.Button
	pathToFile := ""
	fname := ""
	currentInfo := widget.NewLabel("")
	sendFileButton = widget.NewButton("Choose a file to send", func() {
		filename, err := nativedialog.File().Title("Select a file to send").Load()
		pathToFile = filename
		if err == nil {
			fnames := strings.Split(filename, "\\")
			fname = fnames[len(fnames)-1]
			sendFileButton.SetText(fmt.Sprintf("Sending '%s'", fname))
		}
		for {
			if len(filename) > 0 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		codePhrase := utils.GetRandomName()
		codePhrase = "zack1"
		crocOptions := croc.Options{
			SharedSecret: codePhrase,
			IsSender:     true,
			Debug:        false,
			NoPrompt:     true,
			RelayAddress: models.DEFAULT_RELAY,
			Stdout:       false,
			DisableLocal: true,
			RelayPorts:   strings.Split("9009,9010,9011,9012,9013", ","),
		}
		cr, err := croc.New(crocOptions)
		if err != nil {
			return
		}

		finfo, err := os.Stat(pathToFile)
		if err != nil {
			// TODO show something
			return
		}

		currentInfo.SetText("Code phrase: " + codePhrase)
		finished := false
		transfering := false
		prog := dialog.NewProgress("Progress", fmt.Sprintf("Transfering '%s' (%s)", fname, utils.ByteCountDecimal(finfo.Size())), w)
		prog.Hide()
		startTime := time.Now()
		go func() {
			for {
				if finished || cr == nil {
					mbPerSecond := float64(finfo.Size()) / time.Since(startTime).Seconds()
					currentInfo.SetText(fmt.Sprintf("Finished transfer (%s/s).", utils.ByteCountDecimal(int64(mbPerSecond))))
					prog.Hide()
					sendFileButton.SetText("Choose a file to send")
					return
				}
				if cr.Step1ChannelSecured {
					currentInfo.SetText("Channel secured.")
				}
				if cr.Step4FileTransfer && !transfering {
					transfering = true
					currentInfo.SetText("Transfering file.")
					prog.Show()
					startTime = time.Now()
				}
				if cr.Bar != nil {
					prog.SetValue(cr.Bar.State().CurrentPercent)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}()
		err = cr.Send(croc.TransferOptions{
			PathToFiles:      []string{pathToFile},
			KeepPathInRemote: false,
		})
		if err != nil {
			// TODO: do something
		}
		cr = nil
		finished = true
		fmt.Println("send")
	})
	sendScreen := widget.NewVBox(
		widget.NewLabelWithStyle("Send a file", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		sendFileButton,
		currentInfo,
		layout.NewSpacer(),
	)

	var codePhraseToReceive string
	entry := widget.NewEntry()
	entry.OnChanged = func(text string) {
		fmt.Println("Entered", text)
		codePhraseToReceive = text
	}
	entry.SetPlaceHolder("Enter code phrase")
	var receiveFileButtion *widget.Button
	receiveFileButtion = widget.NewButton("Set directory to save", func() {
		filename, err := nativedialog.Directory().Title("Now find a dir").Browse()
		fmt.Println(filename)
		fmt.Println(err)
		receiveFileButtion.SetText(filename)
	})
	receiveScreen := widget.NewVBox(
		widget.NewLabelWithStyle("Receive a file", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		receiveFileButtion,
		entry,
		widget.NewButton("Receive", func() {
			cnf := dialog.NewConfirm("Confirmation", "Accept file ?", confirmCallback, w)
			cnf.SetDismissText("Nah")
			cnf.SetConfirmText("Oh Yes!")
			cnf.Show()
			fmt.Println("codePhraseToReceive")
		}),
		layout.NewSpacer(),
		currentInfo,
		widget.NewHBox(
			widget.NewLabel("Progress:"),
			progress,
		),
	)

	progress.SetValue(0)

	top := makeCell()
	bottom := makeCell()
	left := makeCell()
	right := makeCell()

	borderLayout := layout.NewBorderLayout(top, bottom, left, right)
	sendScreenWrap := fyne.NewContainerWithLayout(borderLayout,
		top, bottom, left, right, sendScreen)
	receiveScreenWrap := fyne.NewContainerWithLayout(borderLayout,
		top, bottom, left, right, receiveScreen)
	welcomeScreenWrap := fyne.NewContainerWithLayout(borderLayout,
		top, bottom, left, right, welcomeScreen(a))

	tabs := widget.NewTabContainer(
		widget.NewTabItemWithIcon("Welcome", theme.HomeIcon(), welcomeScreenWrap),
		widget.NewTabItemWithIcon("Send", theme.MailSendIcon(), sendScreenWrap),
		widget.NewTabItemWithIcon("Receive", theme.MailReplyIcon(), receiveScreenWrap),
	)
	tabs.SetTabLocation(widget.TabLocationLeading)
	w.SetContent(tabs)

	w.ShowAndRun()
}

func confirmCallback(response bool) {
	fmt.Println("Responded with", response)
}
