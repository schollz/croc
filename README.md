<p align="center">
  <img src="https://user-images.githubusercontent.com/6550035/46709024-9b23ad00-cbf6-11e8-9fb2-ca8b20b7dbec.jpg" width="408px" border="0" alt="croc">
  <br>
  <a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/github/v/release/schollz/croc" alt="Version"></a>
  <a href="https://github.com/schollz/croc/actions/workflows/ci.yml"><img src="https://github.com/schollz/croc/actions/workflows/ci.yml/badge.svg" alt="Build Status"></a>
  <a href="https://github.com/sponsors/schollz"><img alt="GitHub Sponsors" src="https://img.shields.io/github/sponsors/schollz"></a>
</p>
<p align="center">
  <strong>This project’s future depends on community support. <a href="https://github.com/sponsors/schollz">Become a sponsor today</a>.</strong>
</p>

## About

`croc` is a tool that allows any two computers to simply and securely transfer files and folders. AFAIK, *croc* is the only CLI file-transfer tool that does **all** of the following:

- Allows **any two computers** to transfer data (using a relay)
- Provides **end-to-end encryption** (using PAKE)
- Enables easy **cross-platform** transfers (Windows, Linux, Mac)
- Allows **multiple file** transfers
- Allows **resuming transfers** that are interrupted
- No need for local server or port-forwarding
- **IPv6-first** with IPv4 fallback
- Can **use a proxy**, like Tor

For more information about `croc`, see [my blog post](https://schollz.com/tinker/croc6/) or read a [recent interview I did](https://console.substack.com/p/console-91).

![Example](src/install/customization.gif)

## Install

You can download [the latest release for your system](https://github.com/schollz/croc/releases/latest), or install a release from the command-line:

```bash
curl https://getcroc.schollz.com | bash
```

### On macOS

Using [Homebrew](https://brew.sh/):

```bash
brew install croc
```

Using [MacPorts](https://www.macports.org/):

```bash
sudo port selfupdate
sudo port install croc
```

### On Windows

You can install the latest release with [Scoop](https://scoop.sh/), [Chocolatey](https://chocolatey.org/), or [Winget](https://learn.microsoft.com/windows/package-manager/):

```bash
scoop install croc
```

```bash
choco install croc
```

```bash
winget install schollz.croc
```

### Using nix-env

You can install the latest release with [Nix](https://nixos.org/):

```bash
nix-env -i croc
```

### On NixOS

You can add this to your [configuration.nix](https://nixos.org/manual/nixos/stable/#ch-configuration):

```nix
environment.systemPackages = [
  pkgs.croc
];
```

### On Alpine Linux

First, install dependencies:

```bash
apk add bash coreutils
wget -qO- https://getcroc.schollz.com | bash
```

### On Arch Linux

Install with `pacman`:

```bash
pacman -S croc
```

### On Fedora

Install with `dnf`:

```bash
dnf install croc
```

### On Gentoo

Install with `portage`:

```bash
emerge net-misc/croc
```

### On Termux

Install with `pkg`:

```bash
pkg install croc
```

### On FreeBSD

Install with `pkg`:

```bash
pkg install croc
```

### On Linux, macOS, and Windows via Conda

You can install from [conda-forge](https://github.com/conda-forge/croc-feedstock) globally with [`pixi`](https://pixi.sh/):

```bash
pixi global install croc
```

Or install into a particular environment with [`conda`](https://docs.conda.io/projects/conda/):

```bash
conda install --channel conda-forge croc
```

### On Linux, macOS via Docker 

Add the following one-liner function to your ~/.profile (works with any POSIX-compliant shell):

```bash
croc() { [ $# -eq 0 ] && set -- ""; mkdir -p "$HOME/.config/croc"; docker run --rm -it --user "$(id -u):$(id -g)" -v "$(pwd):/c" -v "$HOME/.config/croc:/.config/croc" -w /c -e CROC_SECRET docker.io/schollz/croc "$@"; }
```

You can also just paste it in the terminal for current session. On first run Docker will pull the image. `croc` via Docker will only work within the current directory and its subdirectories.

### Build from Source

If you prefer, you can [install Go](https://go.dev/dl/) and build from source (requires Go 1.22+):

```bash
go install github.com/schollz/croc/v10@latest
```

### On Android

There is a 3rd-party F-Droid app [available to download](https://f-droid.org/packages/com.github.howeyc.crocgui/).

## Usage

To send a file, simply do:

```bash
$ croc send [file(s)-or-folder]
Sending 'file-or-folder' (X MB)
Code is: code-phrase
```

Then, to receive the file (or folder) on another computer, run:

```bash
croc code-phrase
```

The code phrase is used to establish password-authenticated key agreement ([PAKE](https://en.wikipedia.org/wiki/Password-authenticated_key_agreement)) which generates a secret key for the sender and recipient to use for end-to-end encryption.

### Customizations & Options

#### Using `croc` on Linux or macOS

On Linux and macOS, the sending and receiving process is slightly different to avoid [leaking the secret via the process name](https://nvd.nist.gov/vuln/detail/CVE-2023-43621). You will need to run `croc` with the secret as an environment variable. For example, to receive with the secret `***`:

```bash
CROC_SECRET=*** croc
```

For single-user systems, the default behavior can be permanently enabled by running:

```bash
croc --classic
```

#### Custom Code Phrase

You can send with your own code phrase (must be more than 6 characters):

```bash
croc send --code [code-phrase] [file(s)-or-folder]
```

#### Allow Overwriting Without Prompt

To automatically overwrite files without prompting, use the `--overwrite` flag:

```bash
croc --yes --overwrite <code>
```

#### Excluding Folders

To exclude folders from being sent, use the `--exclude` flag with comma-delimited exclusions:

```bash
croc send --exclude "node_modules,.venv" [folder]
```

#### Use Pipes - stdin and stdout

You can pipe to `croc`:

```bash
cat [filename] | croc send
```

To receive the file to `stdout`, you can use:

```bash
croc --yes [code-phrase] > out
```

#### Send Text

To send URLs or short text, use:

```bash
croc send --text "hello world"
```

#### Send Multiple Files

You can send multiple files directly by listing the files and/or folders:

```bash
croc send [file1] [file2] [file3] [folder1] [folder2]
```

#### Show QR Code

To show QR code (for mobile devices), use:

```bash
croc send --qr [file(s)-or-folder]
```

#### Use a Proxy

You can send files via a proxy by adding `--socks5`:

```bash
croc --socks5 "127.0.0.1:9050" send SOMEFILE
```

#### Change Encryption Curve

To choose a different elliptic curve for encryption, use the `--curve` flag:

```bash
croc --curve p521 <codephrase>
```

#### Change Hash Algorithm

For faster hashing, use the `imohash` algorithm:

```bash
croc send --hash imohash SOMEFILE
```

#### Clipboard Options

By default, the code phrase is copied to your clipboard. To disable this:

```bash
croc --disable-clipboard send [filename]
```

To copy the full command with the secret as an environment variable (useful on Linux/macOS):

```bash
croc --extended-clipboard send [filename]
```

This copies the full command like `CROC_SECRET="code-phrase" croc` (including any relay/pass flags).

#### Quiet Mode

To suppress all output (useful for scripts and automation):

```bash
croc --quiet send [filename]
```

#### Self-host Relay

You can run your own relay:

```bash
croc relay
```

By default, it uses TCP ports 9009-9013. You can customize the ports (e.g., `croc relay --ports 1111,1112`), but at least **2** ports are required.

To send files using your relay:

```bash
croc --relay "myrelay.example.com:9009" send [filename]
```

#### Self-host Relay with Docker

You can also run a relay with Docker:

```bash
docker run -d -p 9009-9013:9009-9013 -e CROC_PASS='YOURPASSWORD' docker.io/schollz/croc
```

To send files using your custom relay:

```bash
croc --pass YOURPASSWORD --relay "myreal.example.com:9009" send [filename]
```

## Acknowledgements

`croc` has evolved through many iterations, and I am thankful for the contributions! Special thanks to:

- [@warner](https://github.com/warner) for the [idea](https://github.com/magic-wormhole/magic-wormhole)
- [@tscholl2](https://github.com/tscholl2) for the [encryption gists](https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28)
- [@skorokithakis](https://github.com/skorokithakis) for [proxying two connections](https://www.stavros.io/posts/proxying-two-connections-go/)

And many more!
