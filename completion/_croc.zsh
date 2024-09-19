#compdef croc

_croc() {
	local line state

	_arguments -s -S \
        '(--internal-dns)--internal-dns[use a built-in DNS stub resolver rather than the host operating system]' \
        '(--classic)--classic[toggle between the classic mode (insecure due to local attack vector) and new mode (secure)]' \
        '(--remember)--remember[save these settings to reuse next time]' \
        '(--debug)--debug[toggle debug mode]' \
        '(--yes)--yes[automatically agree to all prompts]' \
        '(--stdout)--stdout[redirect file to stdout]' \
        '(--no-compress)--no-compress[disable compression]' \
        '(--ask)--ask[make sure sender and recipient are prompted]' \
        '(--local)--local[force to use only local connections]' \
        '(--ignore-stdin)--ignore-stdin[ignore piped stdin]' \
        '(--overwrite)--overwrite[do not prompt to overwrite or resume]' \
        '(--testing)--testing[flag for testing purposes]' \
        '(--curve)--curve value[choose an encryption curve(p521, p256, p384, siec)]' \
        '(--ip)--ip value[set sender ip if known e.g. 10.0.0.1:9009, [::1]:9009]' \
        '(--relay)--relay value[IPv4 address of the relay]' \
        '(--relay6)--relay6 value[ipv6 address of the relay]' \
        '(--out)--out value[specify an output folder to receive the file]' \
        '(--pass)--pass value[password for the relay]' \
        '(--socks5)--socks5 value[add a socks5 proxy]' \
        '(--connect)--connect[add a http proxy]' \
        '(--throttleUpload)--throttleUpload[Throttle the upload speed e.g. 500k]' \
        '(-h --help)'{-h,--help}'[show help]' \
        '(-v --version)'{-v,--version}'[print the version]' \
        '1: :->cmds' \
        "*::arg:->args" \

	case "$state" in
		cmds)
			_values "croc command" \
				"send[send file(s), or folder (see options with croc send -h)]" \
                "completion[Generate shell completions]"
                "relay[start your own relay (optional)]" \
                "help[Shows a list of commands or help for one command]" \
			;;
		args)
			case $line[1] in
				send)
					_croc_send_cmd
				;;
				relay)
					_croc_relay_cmd
				;;
				help)
					_croc_help_cmd
				;;
			esac
			;;
	esac
}

_croc_send_cmd() {
    local state line curcontext=$curcontext

	_arguments -s -S \
        '(--zip)--zip[zip folder before sending]' \
        '(-c --code)'{-c,--code}'[codephrase used to connect to relay]' \
        '(--hash)--hash[hash algorithm (xxhash, imohash, md5)]' \
        '(-t --text)'{-t,--text}'[send some text]' \
        '(--no-local)--no-local[disable local relay when sending]' \
        '(--no-multi)--no-multi[disable multiplexing]' \
        "(--git)--git[enable .gitignore respect \/ don\'t send ignored files]" \
        '(--port)--port[base port for the relay]' \
        '(--transfers)--transfers[number of ports to use for transfers]' \
        '(--delete)--delete[delete all the files after transfer is complete]' \
        '(-h --help)'{-h,--help}'[show help]' \
        '*:: :->file'

	case "$state" in
          (file)
                (( CURRENT > 0 )) && line[CURRENT]=()
                line=( ${line//(#m)[\[\]()\\*?#<>~\^\|]/\\$MATCH} )
                _files -F line
          ;;
	esac
}

_croc_relay_cmd() {
    local line state

	_arguments -s -S \
        '(--host)--host[host of the relay]' \
        '(--ports)--ports[ports of the relay]' \
        '(--port)--port[base port for the relay]' \
        '(--transfers)--transfers[number of ports to use for relay]' \
        '(-h --help)'{-h,--help}'[show help]' \
        '1: :->cmds' \
        "*::arg:->args" \

    case "$state" in
        cmds)
            _values "croc command" \
                "send[send file(s), or folder (see options with croc send -h)]" \
                "help[Shows a list of commands or help for one command]" \
            ;;   
        args)
			case $line[1] in
				send)
					_croc_send_cmd
				;;
				help)
					_croc_help_cmd
				;;
			esac
			;;
    esac
}

_croc_help_cmd() {
    local line state

    _arguments -s -S \
        '(-h --help)'{-h,--help}'[show help]' \
}

compdef _croc croc
