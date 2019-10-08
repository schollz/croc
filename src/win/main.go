package main

import (
	"fmt"
	"image"
	"net/url"
	"os"
	"strings"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	nativedialog "github.com/sqweek/dialog"
)

var logoImage image.Image

func init() {
	infile, err := os.Open("croc.png")
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

func makeFormTab() fyne.Widget {
	name := widget.NewEntry()
	name.SetPlaceHolder("John Smith")
	email := widget.NewEntry()
	email.SetPlaceHolder("test@example.com")
	password := widget.NewPasswordEntry()
	password.SetPlaceHolder("Password")
	largeText := widget.NewMultiLineEntry()

	form := &widget.Form{
		OnCancel: func() {
			fmt.Println("Cancelled")
		},
		OnSubmit: func() {
			fmt.Println("Form submitted")
			fmt.Println("Name:", name.Text)
			fmt.Println("Email:", email.Text)
			fmt.Println("Password:", password.Text)
			fmt.Println("Message:", largeText.Text)
		},
	}
	form.Append("Name", name)
	form.Append("Email", email)
	form.Append("Password", password)
	form.Append("Message", largeText)

	return form
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
	sendFileButton = widget.NewButton("Select file", func() {
		filename, err := nativedialog.File().Title("Select a file to send").Load()
		if err == nil {
			fnames := strings.Split(filename, "\\")
			sendFileButton.SetText(fnames[len(fnames)-1])
		}
	})
	currentInfo := widget.NewLabel("")

	sendScreen := widget.NewVBox(
		widget.NewLabelWithStyle("Send a file", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		sendFileButton,
		widget.NewButton("Send", func() {
			fmt.Println("send")
		}),
		layout.NewSpacer(),
		currentInfo,
		widget.NewHBox(
			widget.NewLabel("Progress:"),
			progress,
		),
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
