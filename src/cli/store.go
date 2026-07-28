package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schollz/cli/v2"
	"github.com/schollz/croc/v10/src/croc"
	"github.com/schollz/croc/v10/src/storeclient"
	"github.com/schollz/croc/v10/src/storecrypto"
	"github.com/schollz/croc/v10/src/utils"
)

type storeReceipt struct {
	ID          string    `json:"id"`
	Origin      string    `json:"origin"`
	UploadToken string    `json:"uploadToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func storeReceiptsPath(require bool) (string, error) {
	directory, err := utils.GetConfigDir(require)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "store-receipts.json"), nil
}

func readStoreReceipts() ([]storeReceipt, error) {
	path, err := storeReceiptsPath(false)
	if err != nil {
		return nil, err
	}
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipts []storeReceipt
	if err = json.Unmarshal(bytes, &receipts); err != nil {
		return nil, err
	}
	now := time.Now()
	active := receipts[:0]
	for _, receipt := range receipts {
		if receipt.ExpiresAt.After(now) {
			active = append(active, receipt)
		}
	}
	return active, nil
}

func writeStoreReceipts(receipts []storeReceipt) error {
	path, err := storeReceiptsPath(true)
	if err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(receipts, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".store-receipts-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	_ = temp.Chmod(0o600)
	if _, err = temp.Write(bytes); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func saveStoreReceipt(receipt storeReceipt) error {
	receipts, err := readStoreReceipts()
	if err != nil {
		return err
	}
	filtered := receipts[:0]
	for _, existing := range receipts {
		if existing.ID != receipt.ID {
			filtered = append(filtered, existing)
		}
	}
	return writeStoreReceipts(append(filtered, receipt))
}

func removeStoreReceipt(id string) error {
	receipts, err := readStoreReceipts()
	if err != nil {
		return err
	}
	filtered := receipts[:0]
	for _, receipt := range receipts {
		if receipt.ID != id {
			filtered = append(filtered, receipt)
		}
	}
	return writeStoreReceipts(filtered)
}

func storedCallbacks(quiet bool) storeclient.Callbacks {
	lastStatus := ""
	return storeclient.Callbacks{
		Status: func(value string) {
			if quiet || value == lastStatus {
				return
			}
			lastStatus = value
			fmt.Fprintf(os.Stderr, "\r%s", value)
		},
		Progress: func(value storeclient.Progress) {
			if quiet || value.TotalSize == 0 {
				return
			}
			percent := float64(value.TotalBytes) / float64(value.TotalSize) * 100
			fmt.Fprintf(
				os.Stderr,
				"\r%s — %.1f%% (%s / %s)",
				value.FileName,
				percent,
				utils.ByteCountDecimal(value.TotalBytes),
				utils.ByteCountDecimal(value.TotalSize),
			)
		},
	}
}

func sendStored(c *cli.Context) error {
	if c.String("text") != "" || c.Bool("zip") || c.Bool("git") ||
		c.String("exclude") != "" || c.String("exclude-file") != "" {
		return errors.New("stored mode supports regular file arguments only")
	}
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 && !c.Bool("ignore-stdin") {
		return errors.New("stored mode does not accept stdin; pass regular file paths")
	}
	paths := c.Args().Slice()
	if len(paths) == 0 {
		return errors.New("must specify file: croc send --store [filename(s)]")
	}

	client := new(storeclient.Client)
	result, err := client.Upload(
		context.Background(),
		strings.TrimSpace(c.String("store-url")),
		paths,
		storedCallbacks(c.Bool("quiet")),
	)
	if !c.Bool("quiet") {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return err
	}
	browserURL, err := result.Share.BrowserURL()
	if err != nil {
		return err
	}
	token, err := result.Share.CLIToken()
	if err != nil {
		return err
	}
	if err = saveStoreReceipt(storeReceipt{
		ID:          result.Share.ID,
		Origin:      result.Share.Origin,
		UploadToken: result.UploadToken,
		ExpiresAt:   result.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("save stored-transfer revoke receipt: %w", err)
	}
	fmt.Fprintf(os.Stderr, `Stored transfer is encrypted and available until %[1]s or one verified download.

Browser link:
    %[2]s

CLI recipient:
    Run croc, then paste this token:
    %[3]s

Revoke before download:
    croc --revoke %[4]s
`, result.ExpiresAt.Local().Format(time.RFC1123), browserURL, token, result.Share.ID)
	if !c.Bool("disable-clipboard") {
		croc.CopyToClipboard(browserURL, c.Bool("quiet"), false)
	}
	if c.Bool("qrcode") {
		croc.ShowReceiveCommandQrCode(browserURL)
	}
	return nil
}

func receiveStored(c *cli.Context, value string) error {
	share, err := storecrypto.ParseShare(value)
	if err != nil {
		return err
	}
	if c.Bool("stdout") {
		return errors.New("--stdout is not supported for stored transfers")
	}
	client := new(storeclient.Client)
	manifest, expires, err := client.Inspect(context.Background(), share)
	if err != nil {
		return err
	}
	total := int64(0)
	fmt.Fprintf(os.Stderr, "Encrypted stored transfer")
	if !expires.IsZero() {
		fmt.Fprintf(os.Stderr, " (expires %s)", expires.Local().Format(time.RFC1123))
	}
	fmt.Fprintln(os.Stderr, ":")
	for _, file := range manifest.Files {
		total += file.Size
		fmt.Fprintf(os.Stderr, "  %s  %s\n", file.Name, utils.ByteCountDecimal(file.Size))
	}
	fmt.Fprintf(os.Stderr, "Total: %s\n", utils.ByteCountDecimal(total))

	output := c.String("out")
	if output == "" {
		output = "."
	}
	for _, file := range manifest.Files {
		destination := filepath.Join(output, file.Name)
		if _, statErr := os.Stat(destination); statErr == nil && !c.Bool("overwrite") {
			if c.Bool("yes") {
				return fmt.Errorf("destination already exists (use --overwrite): %s", destination)
			}
			choice, inputErr := utils.GetInput(fmt.Sprintf("Replace %s? (y/N) ", destination))
			if inputErr != nil || !strings.EqualFold(strings.TrimSpace(choice), "y") {
				return errors.New("stored transfer refused")
			}
		}
	}
	if !c.Bool("yes") {
		choice, inputErr := utils.GetInput("Receive these files? (Y/n) ")
		if inputErr != nil {
			return inputErr
		}
		normalized := strings.ToLower(strings.TrimSpace(choice))
		if normalized != "" && normalized != "y" && normalized != "yes" {
			return errors.New("stored transfer refused")
		}
	}
	err = client.Receive(
		context.Background(),
		share,
		manifest,
		output,
		storedCallbacks(c.Bool("quiet")),
	)
	if !c.Bool("quiet") {
		fmt.Fprintln(os.Stderr)
	}
	return err
}

func revokeStored(c *cli.Context, transferID string) error {
	setDebugLevel(c)
	id := strings.TrimSpace(transferID)
	if id == "" || c.Args().Present() {
		return errors.New("usage: croc --revoke [transfer-id]")
	}
	receipts, err := readStoreReceipts()
	if err != nil {
		return err
	}
	var receipt *storeReceipt
	for index := range receipts {
		if receipts[index].ID == id {
			receipt = &receipts[index]
			break
		}
	}
	if receipt == nil {
		return fmt.Errorf("no unexpired local revoke receipt for %s", id)
	}
	share := storecrypto.Share{
		Origin:    receipt.Origin,
		ID:        receipt.ID,
		MasterKey: make([]byte, storecrypto.KeySize),
	}
	err = new(storeclient.Client).Revoke(context.Background(), share, receipt.UploadToken)
	var httpErr *storeclient.HTTPError
	if err != nil && !(errors.As(err, &httpErr) &&
		(httpErr.StatusCode == http.StatusGone || httpErr.StatusCode == http.StatusNotFound)) {
		return err
	}
	if err = removeStoreReceipt(id); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Stored transfer %s revoked.\n", id)
	return nil
}
