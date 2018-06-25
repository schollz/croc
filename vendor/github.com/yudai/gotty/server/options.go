package server

import (
	"github.com/pkg/errors"
)

type Options struct {
	Address             string           `hcl:"address" flagName:"address" flagSName:"a" flagDescribe:"IP address to listen" default:"0.0.0.0"`
	Port                string           `hcl:"port" flagName:"port" flagSName:"p" flagDescribe:"Port number to liten" default:"8080"`
	PermitWrite         bool             `hcl:"permit_write" flagName:"permit-write" flagSName:"w" flagDescribe:"Permit clients to write to the TTY (BE CAREFUL)" default:"false"`
	EnableBasicAuth     bool             `hcl:"enable_basic_auth" default:"false"`
	Credential          string           `hcl:"credential" flagName:"credential" flagSName:"c" flagDescribe:"Credential for Basic Authentication (ex: user:pass, default disabled)" default:""`
	EnableRandomUrl     bool             `hcl:"enable_random_url" flagName:"random-url" flagSName:"r" flagDescribe:"Add a random string to the URL" default:"false"`
	RandomUrlLength     int              `hcl:"random_url_length" flagName:"random-url-length" flagDescribe:"Random URL length" default:"8"`
	EnableTLS           bool             `hcl:"enable_tls" flagName:"tls" flagSName:"t" flagDescribe:"Enable TLS/SSL" default:"false"`
	TLSCrtFile          string           `hcl:"tls_crt_file" flagName:"tls-crt" flagDescribe:"TLS/SSL certificate file path" default:"~/.gotty.crt"`
	TLSKeyFile          string           `hcl:"tls_key_file" flagName:"tls-key" flagDescribe:"TLS/SSL key file path" default:"~/.gotty.key"`
	EnableTLSClientAuth bool             `hcl:"enable_tls_client_auth" default:"false"`
	TLSCACrtFile        string           `hcl:"tls_ca_crt_file" flagName:"tls-ca-crt" flagDescribe:"TLS/SSL CA certificate file for client certifications" default:"~/.gotty.ca.crt"`
	IndexFile           string           `hcl:"index_file" flagName:"index" flagDescribe:"Custom index.html file" default:""`
	TitleFormat         string           `hcl:"title_format" flagName:"title-format" flagSName:"" flagDescribe:"Title format of browser window" default:"{{ .command }}@{{ .hostname }}"`
	EnableReconnect     bool             `hcl:"enable_reconnect" flagName:"reconnect" flagDescribe:"Enable reconnection" default:"false"`
	ReconnectTime       int              `hcl:"reconnect_time" flagName:"reconnect-time" flagDescribe:"Time to reconnect" default:"10"`
	MaxConnection       int              `hcl:"max_connection" flagName:"max-connection" flagDescribe:"Maximum connection to gotty" default:"0"`
	Once                bool             `hcl:"once" flagName:"once" flagDescribe:"Accept only one client and exit on disconnection" default:"false"`
	Timeout             int              `hcl:"timeout" flagName:"timeout" flagDescribe:"Timeout seconds for waiting a client(0 to disable)" default:"0"`
	PermitArguments     bool             `hcl:"permit_arguments" flagName:"permit-arguments" flagDescribe:"Permit clients to send command line arguments in URL (e.g. http://example.com:8080/?arg=AAA&arg=BBB)" default:"true"`
	Preferences         *HtermPrefernces `hcl:"preferences"`
	Width               int              `hcl:"width" flagName:"width" flagDescribe:"Static width of the screen, 0(default) means dynamically resize" default:"0"`
	Height              int              `hcl:"height" flagName:"height" flagDescribe:"Static height of the screen, 0(default) means dynamically resize" default:"0"`
	WSOrigin            string           `hcl:"ws_origin" flagName:"ws-origin" flagDescribe:"A regular expression that matches origin URLs to be accepted by WebSocket. No cross origin requests are acceptable by default" default:""`
	Term                string           `hcl:"term" flagName:"term" flagDescribe:"Terminal name to use on the browser, one of xterm or hterm." default:"xterm"`

	TitleVariables map[string]interface{}
}

func (options *Options) Validate() error {
	if options.EnableTLSClientAuth && !options.EnableTLS {
		return errors.New("TLS client authentication is enabled, but TLS is not enabled")
	}
	return nil
}

type HtermPrefernces struct {
	AltGrMode                     *string                      `hcl:"alt_gr_mode" json:"alt-gr-mode,omitempty"`
	AltBackspaceIsMetaBackspace   bool                         `hcl:"alt_backspace_is_meta_backspace" json:"alt-backspace-is-meta-backspace,omitempty"`
	AltIsMeta                     bool                         `hcl:"alt_is_meta" json:"alt-is-meta,omitempty"`
	AltSendsWhat                  string                       `hcl:"alt_sends_what" json:"alt-sends-what,omitempty"`
	AudibleBellSound              string                       `hcl:"audible_bell_sound" json:"audible-bell-sound,omitempty"`
	DesktopNotificationBell       bool                         `hcl:"desktop_notification_bell" json:"desktop-notification-bell,omitempty"`
	BackgroundColor               string                       `hcl:"background_color" json:"background-color,omitempty"`
	BackgroundImage               string                       `hcl:"background_image" json:"background-image,omitempty"`
	BackgroundSize                string                       `hcl:"background_size" json:"background-size,omitempty"`
	BackgroundPosition            string                       `hcl:"background_position" json:"background-position,omitempty"`
	BackspaceSendsBackspace       bool                         `hcl:"backspace_sends_backspace" json:"backspace-sends-backspace,omitempty"`
	CharacterMapOverrides         map[string]map[string]string `hcl:"character_map_overrides" json:"character-map-overrides,omitempty"`
	CloseOnExit                   bool                         `hcl:"close_on_exit" json:"close-on-exit,omitempty"`
	CursorBlink                   bool                         `hcl:"cursor_blink" json:"cursor-blink,omitempty"`
	CursorBlinkCycle              [2]int                       `hcl:"cursor_blink_cycle" json:"cursor-blink-cycle,omitempty"`
	CursorColor                   string                       `hcl:"cursor_color" json:"cursor-color,omitempty"`
	ColorPaletteOverrides         []*string                    `hcl:"color_palette_overrides" json:"color-palette-overrides,omitempty"`
	CopyOnSelect                  bool                         `hcl:"copy_on_select" json:"copy-on-select,omitempty"`
	UseDefaultWindowCopy          bool                         `hcl:"use_default_window_copy" json:"use-default-window-copy,omitempty"`
	ClearSelectionAfterCopy       bool                         `hcl:"clear_selection_after_copy" json:"clear-selection-after-copy,omitempty"`
	CtrlPlusMinusZeroZoom         bool                         `hcl:"ctrl_plus_minus_zero_zoom" json:"ctrl-plus-minus-zero-zoom,omitempty"`
	CtrlCCopy                     bool                         `hcl:"ctrl_c_copy" json:"ctrl-c-copy,omitempty"`
	CtrlVPaste                    bool                         `hcl:"ctrl_v_paste" json:"ctrl-v-paste,omitempty"`
	EastAsianAmbiguousAsTwoColumn bool                         `hcl:"east_asian_ambiguous_as_two_column" json:"east-asian-ambiguous-as-two-column,omitempty"`
	Enable8BitControl             *bool                        `hcl:"enable_8_bit_control" json:"enable-8-bit-control,omitempty"`
	EnableBold                    *bool                        `hcl:"enable_bold" json:"enable-bold,omitempty"`
	EnableBoldAsBright            bool                         `hcl:"enable_bold_as_bright" json:"enable-bold-as-bright,omitempty"`
	EnableClipboardNotice         bool                         `hcl:"enable_clipboard_notice" json:"enable-clipboard-notice,omitempty"`
	EnableClipboardWrite          bool                         `hcl:"enable_clipboard_write" json:"enable-clipboard-write,omitempty"`
	EnableDec12                   bool                         `hcl:"enable_dec12" json:"enable-dec12,omitempty"`
	Environment                   map[string]string            `hcl:"environment" json:"environment,omitempty"`
	FontFamily                    string                       `hcl:"font_family" json:"font-family,omitempty"`
	FontSize                      int                          `hcl:"font_size" json:"font-size,omitempty"`
	FontSmoothing                 string                       `hcl:"font_smoothing" json:"font-smoothing,omitempty"`
	ForegroundColor               string                       `hcl:"foreground_color" json:"foreground-color,omitempty"`
	HomeKeysScroll                bool                         `hcl:"home_keys_scroll" json:"home-keys-scroll,omitempty"`
	Keybindings                   map[string]string            `hcl:"keybindings" json:"keybindings,omitempty"`
	MaxStringSequence             int                          `hcl:"max_string_sequence" json:"max-string-sequence,omitempty"`
	MediaKeysAreFkeys             bool                         `hcl:"media_keys_are_fkeys" json:"media-keys-are-fkeys,omitempty"`
	MetaSendsEscape               bool                         `hcl:"meta_sends_escape" json:"meta-sends-escape,omitempty"`
	MousePasteButton              *int                         `hcl:"mouse_paste_button" json:"mouse-paste-button,omitempty"`
	PageKeysScroll                bool                         `hcl:"page_keys_scroll" json:"page-keys-scroll,omitempty"`
	PassAltNumber                 *bool                        `hcl:"pass_alt_number" json:"pass-alt-number,omitempty"`
	PassCtrlNumber                *bool                        `hcl:"pass_ctrl_number" json:"pass-ctrl-number,omitempty"`
	PassMetaNumber                *bool                        `hcl:"pass_meta_number" json:"pass-meta-number,omitempty"`
	PassMetaV                     bool                         `hcl:"pass_meta_v" json:"pass-meta-v,omitempty"`
	ReceiveEncoding               string                       `hcl:"receive_encoding" json:"receive-encoding,omitempty"`
	ScrollOnKeystroke             bool                         `hcl:"scroll_on_keystroke" json:"scroll-on-keystroke,omitempty"`
	ScrollOnOutput                bool                         `hcl:"scroll_on_output" json:"scroll-on-output,omitempty"`
	ScrollbarVisible              bool                         `hcl:"scrollbar_visible" json:"scrollbar-visible,omitempty"`
	ScrollWheelMoveMultiplier     int                          `hcl:"scroll_wheel_move_multiplier" json:"scroll-wheel-move-multiplier,omitempty"`
	SendEncoding                  string                       `hcl:"send_encoding" json:"send-encoding,omitempty"`
	ShiftInsertPaste              bool                         `hcl:"shift_insert_paste" json:"shift-insert-paste,omitempty"`
	UserCss                       string                       `hcl:"user_css" json:"user-css,omitempty"`
}
