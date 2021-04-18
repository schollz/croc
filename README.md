
<p align="center">
<img
    src="https://user-images.githubusercontent.com/6550035/46709024-9b23ad00-cbf6-11e8-9fb2-ca8b20b7dbec.jpg"
    width="408px" border="0" alt="croc">
<br>
<a href="https://github.com/schollz/croc/releases/latest"><img src="https://img.shields.io/badge/version-v9.0.0-brightgreen.svg?style=flat-square" alt="Version"></a>
<a href="https://coveralls.io/github/schollz/croc"><img src="https://img.shields.io/badge/coverage-81%25-green.svg?style=flat-square" alt="Coverage"></a>
<a href="https://travis-ci.org/schollz/croc"><img
src="https://img.shields.io/travis/schollz/croc.svg?style=flat-square" alt="Build
Status"></a> 
<p align="center">This project is supported by:</p>
<p align="center">
  <a href="https://www.digitalocean.com/">
    <img src="https://opensource.nyc3.cdn.digitaloceanspaces.com/attribution/assets/SVG/DO_Logo_horizontal_blue.svg" width="201px">
  </a>
</p>
</p>

`croc` is a tool that allows any two computers to simply and securely transfer files and folders. AFAIK, *croc* is the only CLI file-transfer tool that does **all** of the following:

- allows **any two computers** to transfer data (using a relay)
- provides **end-to-end encryption** (using PAKE)
- enables easy **cross-platform** transfers (Windows, Linux, Mac)
- allows **multiple file** transfers
- allows **resuming transfers** that are interrupted
- local server or port-forwarding **not needed**
- **ipv6-first** with ipv4 fallback
- can **use proxy**, like tor

For more information about `croc`, see [my blog post](https://schollz.com/software/croc6).

![Example](src/install/customization.gif)

## Install

Download [the latest release for your system](https://github.com/schollz/croc/releases/latest), or install a release from the command-line:

```
curl https://getcroc.schollz.com | bash
```


On macOS you can install the latest release with [Homebrew](https://brew.sh/): 

```
brew install croc
```

On macOS you can also install the latest release with [MacPorts](https://macports.org/): 

```
sudo port selfupdate
sudo port install croc
```

On Windows you can install the latest release with [Scoop](https://scoop.sh/) or [Chocolatey](https://chocolatey.org): 

```
scoop install croc
```

```
choco install croc
```

On Unix you can install the latest release with [Nix](https://nixos.org/nix):

```
nix-env -i croc
```


On Alpine Linux you have to install dependencies first:

```
apk add bash coreutils
wget -qO- https://getcroc.schollz.com | bash
```

On Arch Linux you can install the latest release with `pacman`:

```
pacman -S croc
```

On Gentoo you can install with `portage`:
```
emerge net-misc/croc
```

On Termux you can install with `pkg`:

```
pkg install croc
```

On FreeBSD you can install with `pkg`:

```
pkg install croc
```

Or, you can [install Go](https://golang.org/dl/) and build from source (requires Go 1.12+): 

```
GO111MODULE=on go get -v github.com/schollz/croc/v9
```

## Usage 

To send a file, simply do: 

```
$ croc send [file(s)-or-folder]
Sending 'file-or-folder' (X MB)
Code is: code-phrase
```

Then to receive the file (or folder) on another computer, you can just do 

```
croc code-phrase
```

The code phrase is used to establish password-authenticated key agreement ([PAKE](https://en.wikipedia.org/wiki/Password-authenticated_key_agreement)) which generates a secret key for the sender and recipient to use for end-to-end encryption.

There are a number of configurable options (see `--help`). A set of options (like custom relay, ports, and code phrase) can be set using `--remember`.

### Custom code phrase

You can send with your own code phrase (must be more than 6 characters).

```
croc send --code [code-phrase] [file(s)-or-folder]
```

### Allow overwriting without prompt

By default, croc will prompt whether to overwrite a file. You can automatically overwrite files by using the `--overwrite` flag (recipient only). For example, receive a file to automatically overwrite:

```
croc --yes --overwrite <code>
```


### Use pipes - stdin and stdout

You can pipe to `croc`:

```
cat [filename] | croc send
```

In this case `croc` will automatically use the stdin data and send and assign a filename like "croc-stdin-123456789". To receive to `stdout` at you can always just use the `--yes`  will automatically approve the transfer and pipe it out to `stdout`. 

```
croc --yes [code-phrase] > out
```

All of the other text printed to the console is going to `stderr` so it will not interfere with the message going to `stdout`.


### Send text 

Sometimes you want to send URLs or short text. In addition to piping, you can easily send text with `croc`:

```
croc send --text "hello world"
```

This will automatically tell the receiver to use `stdout` when they receive the text so it will be displayed.


### Use a proxy

You can use a proxy as your connection to the relay by adding a proxy address with `--socks5`. For example, you can send via a tor relay:

```
croc --socks5 "127.0.0.1:9050" send SOMEFILE
```

### Change encryption curve

You can choose from several different elliptic curves to use for encryption by using the `--curve` flag. Only the recipient can choose the curve. For example, receive a file using the P-521 curve:

```
croc --curve p521 <codephrase>
```

Available curves are P-256, P-348, P-521 and SIEC. SIEC is the default curve used, it is a lesser known curve that belongs to a class of "super-isolated" curves which has security that does not reduce to the security of curves around it. (Scholl, Travis. Experimental Mathematics 28.4 (2019): 385-397)


### Self-host relay

The relay is needed to staple the parallel incoming and outgoing connections. By default, `croc` uses a public relay but you can also run your own relay:

```
croc relay
```

By default it uses TCP ports 9009-9013. Make sure to open those up. You can customized the ports (e.g. `croc relay --ports 1111,1112`), but you must have a minimum of **2** ports for the relay. The first port is for communication and the subsequent ports are used for the multiplexed data transfer.

You can send files using your relay by entering `--relay` to change the relay that you are using if you want to custom host your own.

```
croc --relay "myrelay.example.com:9009" send [filename]
```

Note, when sending, you only need to include the first port (the communication port). The subsequent ports for data transfer will be transmitted back to the user from the relay.

For systems that use systemd you can use the included service file to run croc automatically and with reduced system privileges.

* Save the `croc.service` file to `/etc/systemd/system`.
* Edit the `Environment="CROC_PASS...` line according to your relay password.
* If your croc binary is located in a path other than `/usr/local/bin` you will need to amend the `ExecPath`, `TemporaryFileSystem` and `BindReadOnlyPaths` directives accordingly.
* Amend the `User` and `Group` directives to reflect the system account that your relay uses.
* Issue `# systemctl daemon-reload` after any changes to the service file.
* Issue `# systemctl enable --now croc` to enable automatic start and to start the relay.
* `$ journalctl -u croc` will list any problems starting the service and `$ systemctl status croc` will display the current state of the relay.

#### Self-host relay (docker)

If it's easier you can also run a relay with Docker:


```
docker run -d -p 9009-9013:9009-9013 -e CROC_PASS='YOURPASSWORD' schollz/croc
```

Be sure to include the password for the relay otherwise any requests will be rejected. 

```
croc --pass YOURPASSWORD --relay "myreal.example.com:9009" send [filename]
```

Note: when including `--pass YOURPASSWORD` you can instead pass a file with the password, e.g. `--pass FILEWITHPASSWORD`.

## License

MIT

## Acknowledgements

`croc` has gone through many iterations, and I am awed by all the great contributions! If you feel like contributing, in any way, by all means you can send an Issue, a PR, ask a question, or tweet me ([@yakczar](http://ctt.ec/Rq054)).

Thanks [@warner](https://github.com/warner) for the [idea](https://github.com/warner/magic-wormhole), [@tscholl2](https://github.com/tscholl2) for the [encryption gists](https://gist.github.com/tscholl2/dc7dc15dc132ea70a98e8542fefffa28), [@skorokithakis](https://github.com/skorokithakis) for [code on proxying two connections](https://www.stavros.io/posts/proxying-two-connections-go/). Finally thanks for making pull requests [@maximbaz](https://github.com/maximbaz), [@meyermarcel](https://github.com/meyermarcel), [@Girbons](https://github.com/Girbons), [@techtide](https://github.com/techtide), [@heymatthew](https://github.com/heymatthew), [@Lunsford94](https://github.com/Lunsford94), [@lummie](https://github.com/lummie), [@jesuiscamille](https://github.com/jesuiscamille), [@threefjord](https://github.com/threefjord), [@marcossegovia](https://github.com/marcossegovia), [@csleong98](https://github.com/csleong98), [@afotescu](https://github.com/afotescu), [@callmefever](https://github.com/callmefever), [@El-JojA](https://github.com/El-JojA), [@anatolyyyyyy](https://github.com/anatolyyyyyy), [@goggle](https://github.com/goggle), [@smileboywtu](https://github.com/smileboywtu), [@nicolashardy](https://github.com/nicolashardy), [@fbartels](https://github.com/fbartels), [@rkuprov](https://github.com/rkuprov), [@hreese](https://github.com/hreese), [@xenrox](https://github.com/xenrox) and [Ipar](https://github.com/lpar)!
