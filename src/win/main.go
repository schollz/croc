package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/canvas"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/layout"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	nativedialog "github.com/sqweek/dialog"
)

func welcomeScreen(a fyne.App) fyne.CanvasObject {
	logo := canvas.NewImageFromResource(theme.FyneLogo())
	logo.SetMinSize(fyne.NewSize(128, 128))

	link, err := url.Parse("https://fyne.io/")
	if err != nil {
		fyne.LogError("Could not parse URL", err)
	}

	return widget.NewVBox(
		widget.NewLabelWithStyle("Welcome to the Fyne toolkit demo app", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		layout.NewSpacer(),
		widget.NewHBox(layout.NewSpacer(), logo, layout.NewSpacer()),
		widget.NewHyperlinkWithStyle("fyne.io", link, fyne.TextAlignCenter, fyne.TextStyle{}),
		layout.NewSpacer(),

		widget.NewGroup("Theme",
			fyne.NewContainerWithLayout(layout.NewGridLayout(2),
				widget.NewButton("Dark", func() {
					a.Settings().SetTheme(theme.DarkTheme())
				}),
				widget.NewButton("Light", func() {
					a.Settings().SetTheme(theme.LightTheme())
				}),
			),
		),
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
	w := a.NewWindow("Hello")
	w.Resize(fyne.Size{200, 200})
	out := widget.NewEntry()
	out.Text = "Hello"

	progress := widget.NewProgressBar()
	var sendFileButton *widget.Button
	sendFileButton = widget.NewButton("Select file", func() {
		filename, err := nativedialog.File().Title("Select a file to send").Load()
		if err == nil {
			fnames := strings.Split(filename, "\\")
			sendFileButton.SetText(fnames[len(fnames)-1])
		}
	})

	sendScreen := widget.NewVBox(
		widget.NewLabelWithStyle("Send a file", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		sendFileButton,
		widget.NewButton("Send", func() {
			fmt.Println("send")
		}),
		layout.NewSpacer(),
		widget.NewHBox(
			widget.NewLabel("Code phrase: something"),
		),
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

	box1 := widget.NewVBox(
		widget.NewLabel("Hello Fyne!"),
		makeFormTab(),
		widget.NewButton("Send", func() {
			filename, err := nativedialog.File().Title("Select a file to send").Load()
			fmt.Println(filename)
			fmt.Println(err)
			codeDialog := dialog.NewInformation("Info", "Your passphrase is: x1", w)
			codeDialog.Show()
		}),
		widget.NewButton("Set directory to save", func() {
			filename, err := nativedialog.Directory().Title("Now find a dir").Browse()
			fmt.Println(filename)
			fmt.Println(err)
		}),

		widget.NewButton("Receive", func() {
			filename, err := nativedialog.Directory().Title("Now find a dir").Browse()
			fmt.Println(filename)
			fmt.Println(err)
			prog := dialog.NewProgress("MyProgress", "Nearly there...", w)

			go func() {
				num := 0.0
				for num < 1.0 {
					time.Sleep(50 * time.Millisecond)
					prog.SetValue(num)
					num += 0.01
				}

				prog.SetValue(1)
				prog.Hide()
			}()

			prog.Show()
		}),
		widget.NewButton("Custom", func() {
			content := widget.NewEntry()
			content.SetPlaceHolder("Enter code phrase:")
			content.OnChanged = func(text string) {
				fmt.Println("Entered", text)
			}
			dialog.ShowCustom("Custom dialog", "Done", content, w)
			fmt.Println("closed dialog")
		}),
		widget.NewButton("Error", func() {
			err := errors.New("A dummy error message")
			dialog.ShowError(err, w)
		}),
		widget.NewButton("Confirm", func() {
			cnf := dialog.NewConfirm("Confirmation", "Are you enjoying this demo?", confirmCallback, w)
			cnf.SetDismissText("Nah")
			cnf.SetConfirmText("Oh Yes!")
			cnf.Show()
		}),
	)
	_ = box1

	tabs := widget.NewTabContainer(
		widget.NewTabItemWithIcon("Welcome", theme.HomeIcon(), welcomeScreen(a)),
		widget.NewTabItemWithIcon("Send", theme.MailSendIcon(), sendScreenWrap),
		widget.NewTabItemWithIcon("Receive", theme.MailSendIcon(), receiveScreenWrap),
	)
	tabs.SetTabLocation(widget.TabLocationLeading)
	w.SetContent(tabs)

	w.ShowAndRun()
}

func confirmCallback(response bool) {
	fmt.Println("Responded with", response)
}
