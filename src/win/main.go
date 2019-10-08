package main

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne"
	"fyne.io/fyne/app"
	"fyne.io/fyne/dialog"
	"fyne.io/fyne/theme"
	"fyne.io/fyne/widget"
	nativedialog "github.com/sqweek/dialog"
)

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

func main() {
	a := app.New()
	a.Settings().SetTheme(theme.LightTheme())

	w := a.NewWindow("Hello")
	w.Resize(fyne.Size{800, 600})
	out := widget.NewEntry()
	out.Text = "Hello"

	w.SetContent(widget.NewVBox(
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
	))

	w.ShowAndRun()
}

func confirmCallback(response bool) {
	fmt.Println("Responded with", response)
}
